package limiter

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

func (c *counter) IsExpired() bool {
	return c.ttl < time.Now().Unix()
}

type counterUpdate struct {
	shard uint64
	key   string
}

type counterMetrics struct {
	counterSyncLoopDuration prometheus.Histogram
}

func newCounterMetrics(r prometheus.Registerer) *counterMetrics {
	var m counterMetrics

	m.counterSyncLoopDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "counters_sync_loop_duration_seconds",
		Help:    "Duration of sync loops",
		Buckets: prometheus.ExponentialBuckets(0.00005, 2, 20),
	})

	r.MustRegister(m.counterSyncLoopDuration)
	return &m
}

// CounterService serves the purpose of storing request counters
type CounterService struct {
	shardCount      uint64
	shardedCounters []map[string]*counter
	shardedMutexes  []*sync.RWMutex
	storage         CounterStorage
	logger          *logrus.Logger
	syncChan        chan counterUpdate
	syncWg          *sync.WaitGroup
	metrics         *counterMetrics
}

// NewCounterService creates a new counter service
func NewCounterService(storage CounterStorage, logger *logrus.Logger, registerer prometheus.Registerer) *CounterService {
	metrics := newCounterMetrics(registerer)

	var shards uint64 = 256

	cm := &CounterService{
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

// Add creates or increments a given value on a counter, it also allows the counter to have a ttl
// and to have a synchronization rate to a remote storage
func (cs *CounterService) Add(ctx context.Context, key string, n uint32, ttl int64, syncRate int) (uint32, error) {
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
	return newValue + c.stored, nil
}

// AddSync creates or increments a given value on a counter directly on the storage, bypassing in-memory storage
func (cs *CounterService) AddSync(ctx context.Context, key string, n uint32, ttl int64) (uint32, error) {
	err := cs.storage.Add(ctx, key, n, ttl)
	if err != nil {
		return 0, err
	}

	count, err := cs.storage.Get(ctx, key)
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

	if c.IsExpired() {
		return 0, ErrExpiredCounter
	}

	return atomic.LoadUint32(&c.current) + atomic.LoadUint32(&c.stored), nil
}

// GetSync will get the current counter value stored on a remote storage
func (cs *CounterService) GetSync(ctx context.Context, key string) (uint32, error) {
	count, err := cs.storage.Get(ctx, key)
	if err != nil {
		return 0, err
	}

	return count, nil
}

// Run will start the syncronization process
func (cs *CounterService) Run(cancel chan struct{}) error {
	// spin up sync workers
	for i := 0; i < 20; i++ {
		cs.syncWg.Add(1)
		go cs.syncWorker()
	}

	for {
		select {
		case <-cancel:
			close(cs.syncChan)
			cs.syncWg.Wait()

			return nil
		case <-time.After(500 * time.Millisecond):
			timer := prometheus.NewTimer(cs.metrics.counterSyncLoopDuration)
			now := time.Now().Unix()
			for i := uint64(0); i < cs.shardCount; i++ {

				mux := cs.shardedMutexes[i]
				mux.RLock()
				countersToDelete := make([]string, 0, len(cs.shardedCounters[i]))
				countersToUpdate := make([]string, 0, len(cs.shardedCounters[i]))
				for k, v := range cs.shardedCounters[i] {
					// if key is expired we should remove it
					if v.IsExpired() {
						countersToDelete = append(countersToDelete, k)
						continue
					}

					// counter does not need to be synched as it is ephemeral
					if v.syncRate == -1 {
						continue
					}

					// if counter needs to be synched
					if now-atomic.LoadInt64(&v.lastSync) > int64(v.syncRate) {
						countersToUpdate = append(countersToUpdate, k)
					}
				}
				mux.RUnlock()

				for _, k := range countersToUpdate {
					cs.syncChan <- counterUpdate{i, k}
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

			timer.ObserveDuration()
		}
	}
}

func (cs *CounterService) syncWorker() {
	defer cs.syncWg.Done()

	for kc := range cs.syncChan {
		cs.syncKey(kc.shard, kc.key)
	}
}

func (cs *CounterService) syncKey(shard uint64, key string) {
	mux := cs.shardedMutexes[shard]
	mux.RLock()
	defer mux.RUnlock()
	counter := cs.shardedCounters[shard][key]

	// get current counter value and reset to 0 to continue a new cycle
	oldCurrent := atomic.SwapUint32(&counter.current, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Microsecond)
	defer cancel()

	// atomically increment stored counter
	err := cs.storage.Add(ctx, key, oldCurrent, counter.ttl)
	if err != nil {
		cs.logger.Error(err)
		return
	}

	// fetch stored counter value
	stored, err := cs.storage.Get(context.Background(), key)
	if err != nil {
		cs.logger.Error(err)
		return
	}

	// store it on the remoteCounter
	atomic.SwapUint32(&counter.stored, stored)
	atomic.SwapInt64(&counter.lastSync, time.Now().Unix())
}
