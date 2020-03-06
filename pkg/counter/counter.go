package counter

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/segmentio/fasthash/fnv1a"
	"github.com/sirupsen/logrus"
)

var ErrNonExistingCounter = errors.New("counter does not exist")
var ErrExpiredCounter = errors.New("counter is expired")

type counter struct {
	current  uint32
	stored   uint32
	ttl      int64
	lastSync int64
	syncRate int
}
type counterUpdate struct {
	shard uint64
	key   string
}

type shardUpdates struct {
	shard uint64
	keys  []string
}

type counterMetrics struct {
	counterSyncTotal prometheus.Counter
}

func newCounterMetrics(r prometheus.Registerer) *counterMetrics {
	var m counterMetrics

	m.counterSyncTotal = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "counter_sync_total",
		Help: "Total number of counter synchronizations",
	})

	r.MustRegister(m.counterSyncTotal)
	return &m
}

// CounterService serves the purpose of storing request counters
type CounterService struct {
	shardCount      uint64
	shardedCounters []map[string]*counter
	shardedMutexes  []*sync.RWMutex

	storage CounterStorage
	logger  *logrus.Logger

	metrics *counterMetrics

	syncChan      chan counterUpdate
	syncWg        *sync.WaitGroup
	syncBatchSize int
}

// NewCounterService creates a new counter service
func NewCounterService(
	storage CounterStorage,
	logger *logrus.Logger,
	registerer prometheus.Registerer,
	syncBatchSize int) *CounterService {
	metrics := newCounterMetrics(registerer)

	var shards uint64 = 64

	cm := &CounterService{
		syncBatchSize:   syncBatchSize,
		shardCount:      shards,
		shardedCounters: make([]map[string]*counter, shards),
		shardedMutexes:  make([]*sync.RWMutex, shards),
		storage:         storage,
		logger:          logger,
		syncChan:        make(chan counterUpdate),
		syncWg:          &sync.WaitGroup{},
		metrics:         metrics,
	}

	// initialize shards
	for i := uint64(0); i < shards; i++ {
		cm.shardedCounters[i] = make(map[string]*counter)
		cm.shardedMutexes[i] = &sync.RWMutex{}
	}

	return cm
}

// Increment creates or increments a given value on a counter, it also allows the counter to have a ttl
// and to have a synchronization rate to a remote storage
func (cs *CounterService) Increment(ctx context.Context, key string, n uint32, ttl int64, syncRate int) (uint32, error) {
	shard := fnv1a.HashString64(key) % cs.shardCount
	mux := cs.shardedMutexes[shard]
	mux.Lock()
	defer mux.Unlock()

	c, ok := cs.shardedCounters[shard][key]
	if !ok {
		cs.shardedCounters[shard][key] = &counter{
			current:  n,
			stored:   0,
			ttl:      ttl,
			syncRate: syncRate,
		}
		return n, nil
	}

	newValue := atomic.AddUint32(&c.current, n)
	if syncRate != c.syncRate {
		c.syncRate = syncRate
	}
	return newValue + c.stored, nil
}

// IncrementOnStorage creates or increments a given value on a counter directly on the storage, bypassing in-memory storage
func (cs *CounterService) IncrementOnStorage(ctx context.Context, key string, n uint32, ttl int64) (uint32, error) {
	count, err := cs.storage.Increment(ctx, key, n, ttl)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// Get will get the current counter value for a given key
func (cs *CounterService) Get(ctx context.Context, key string) (uint32, error) {
	shard := fnv1a.HashString64(key) % cs.shardCount
	mux := cs.shardedMutexes[shard]
	mux.RLock()
	defer mux.RUnlock()

	c, ok := cs.shardedCounters[shard][key]
	if !ok {
		return 0, ErrNonExistingCounter
	}

	return atomic.LoadUint32(&c.current) + atomic.LoadUint32(&c.stored), nil
}

// GetFromStorage will get the current counter value stored on a remote storage
func (cs *CounterService) GetFromStorage(ctx context.Context, key string) (uint32, error) {
	count, err := cs.storage.Get(ctx, key)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// RunSync will start the syncronization process
func (cs *CounterService) RunSync(cancel chan struct{}) error {

	// spawn shardAnalyzers
	analyzerCount := 8
	for i := 0; i < analyzerCount; i++ {
		cs.syncWg.Add(1)
		go cs.shardAnalyzer(cancel, i, analyzerCount)
	}

	for i := 0; i < analyzerCount; i++ {
		cs.syncWg.Add(1)
		go cs.syncLoop(cancel)
	}

	<-cancel
	cs.syncWg.Wait()
	close(cs.syncChan)

	return nil
}

func (cs *CounterService) shardAnalyzer(cancel chan struct{}, initShard int, analyzerCount int) {
	defer cs.syncWg.Done()
	for {
		select {
		case <-cancel:
			return
		case <-time.After(500 * time.Millisecond):
			now := time.Now().Unix()

			for i := initShard; i < int(cs.shardCount); i += analyzerCount {

				countersToDelete := make([]string, 0, len(cs.shardedCounters[i]))
				countersToUpdate := make([]string, 0, len(cs.shardedCounters[i]))

				mux := cs.shardedMutexes[i]
				mux.RLock()
				for k, v := range cs.shardedCounters[i] {
					// counter does not need to be synchronized as it only lives in memory
					if v.syncRate == -1 {
						continue
					}

					// if key is expired we should remove it
					if v.ttl < now {
						countersToDelete = append(countersToDelete, k)
						continue
					}

					// if counter needs to be synchronized
					if now-atomic.LoadInt64(&v.lastSync) > int64(v.syncRate) {
						countersToUpdate = append(countersToUpdate, k)
					}
				}
				mux.RUnlock()

				for _, k := range countersToUpdate {
					// there's a select here so the code does not get blocked here
					// if the syncronizer gets a big amount of lag
					select {
					case cs.syncChan <- counterUpdate{uint64(i), k}:
					case <-cancel:
						return
					}
				}

				// We need to remove the keys outside of the loop, as there can be other threads creating new counters
				// at the same time the loop is happening, and adding a rlock in the range statement would make impossible the
				// upgrade rlock for a lock.
				mux.Lock()
				for _, k := range countersToDelete {
					delete(cs.shardedCounters[i], k)
				}
				mux.Unlock()
			}
		}
	}
}

func (cs *CounterService) syncLoop(cancel chan struct{}) {
	batch := make([]CounterInc, 0, cs.syncBatchSize)
	ticker := time.NewTicker(300 * time.Millisecond)
	defer cs.syncWg.Done()
	defer ticker.Stop()

	for {
		needsFlush := false
		select {
		case c := <-cs.syncChan:
			mux := cs.shardedMutexes[c.shard]
			mux.RLock()

			counter := cs.shardedCounters[c.shard][c.key]
			curr := atomic.LoadUint32(&counter.current)

			inc := CounterInc{
				Key: c.key,
				Inc: curr,
				TTL: counter.ttl,
			}

			batch = append(batch, inc)
			mux.RUnlock()

			if len(batch) >= cs.syncBatchSize {
				needsFlush = true
			}
		case <-ticker.C:
			needsFlush = true
		case <-cancel:
			return
		}

		if needsFlush {
			cs.syncCounters(batch)
			batch = batch[0:0]
		}
	}
}

func (cs *CounterService) syncCounters(incs []CounterInc) {
	if len(incs) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1000*time.Millisecond)
	defer cancel()
	incvalues, err := cs.storage.BatchIncrement(ctx, incs)

	if err != nil {
		cs.logger.Error(err)
		return
	}

	for i, inc := range incvalues {
		shard := fnv1a.HashString64(inc.Key) % cs.shardCount
		mux := cs.shardedMutexes[shard]
		mux.RLock()

		counter, ok := cs.shardedCounters[shard][inc.Key]
		if !ok {
			mux.RUnlock()
			continue
		}

		if inc.Err != nil {
			cs.logger.Error(err)
			mux.RUnlock()
			return
		}

		// decrement current counter with the value that was
		// incremented on storage
		atomic.AddUint32(&counter.current, ^uint32(incs[i].Inc-1))
		atomic.SwapUint32(&counter.stored, inc.Curr)
		atomic.SwapInt64(&counter.lastSync, time.Now().Unix())
		cs.metrics.counterSyncTotal.Add(1)

		mux.RUnlock()
	}
}
