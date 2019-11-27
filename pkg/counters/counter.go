package counters

import (
	"context"
	"errors"
	"hash/fnv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

var ErrNonExistingCounter = errors.New("counter does not exist")
var ErrExpiredCounter = errors.New("counter is expired")

type counter struct {
	currCounter   uint32
	remoteCounter uint32
	ttl           int64
}

func (c *counter) IsExpired() bool {
	return c.ttl < time.Now().Unix()
}

type CounterService struct {
	shardCount           uint32
	shardedCounters      []map[string]*counter
	shardedMutexes       []*sync.RWMutex
	remoteCounterStorage RemoteCounterStorage
	logger               *logrus.Logger
}

// NewCounterService creates a new counter service
func NewCounterService(storage RemoteCounterStorage, logger *logrus.Logger, ctx context.Context) *CounterService {
	var shards uint32 = 64
	cm := &CounterService{
		shardCount:           shards,
		shardedCounters:      make([]map[string]*counter, shards),
		shardedMutexes:       make([]*sync.RWMutex, shards),
		remoteCounterStorage: storage,
		logger:               logger,
	}

	// initialize shards
	for i := uint32(0); i < shards; i++ {
		cm.shardedCounters[i] = make(map[string]*counter)
		cm.shardedMutexes[i] = &sync.RWMutex{}
	}

	go cm.syncLoop(ctx)

	return cm
}

// Add or create a counter
func (cs *CounterService) Add(ctx context.Context, key string, n uint32, ttl int64) (uint32, error) {
	hsh := fnv.New32()
	hsh.Write([]byte(key))
	shard := hsh.Sum32() % cs.shardCount

	mux := cs.shardedMutexes[shard]
	mux.Lock()
	defer mux.Unlock()

	c, ok := cs.shardedCounters[shard][key]
	if !ok {
		cs.shardedCounters[shard][key] = &counter{
			currCounter:   n,
			remoteCounter: 0,
			ttl:           ttl,
		}

		return n, nil
	}

	newValue := atomic.AddUint32(&c.currCounter, n)

	return newValue + c.remoteCounter, nil
}

// Peek will see the current counter value for a given key
func (cs *CounterService) Peek(ctx context.Context, key string) (uint32, error) {
	hsh := fnv.New32()
	hsh.Write([]byte(key))
	shard := hsh.Sum32() % cs.shardCount

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

	return c.currCounter + c.remoteCounter, nil
}

func (cs *CounterService) syncLoop(ctx context.Context) {
	ticker := time.NewTicker(1000 * time.Millisecond)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// iterate over the shards to remove everything that is expired
			for i := uint32(0); i < cs.shardCount; i++ {
				cs.shardedMutexes[i].RLock()
				for k, v := range cs.shardedCounters[i] {
					cs.syncKey(ctx, k, v, i)
				}
				cs.shardedMutexes[i].RUnlock()
			}
		}
	}
}

func (cs *CounterService) syncKey(ctx context.Context, key string, value *counter, shard uint32) {
	if value.IsExpired() {
		cs.shardedMutexes[shard].Lock()
		delete(cs.shardedCounters[shard], key)
		cs.shardedMutexes[shard].Unlock()
		return
	}

	// get current counter value and reset to 0 to continue a new cycle
	old := atomic.SwapUint32(&value.currCounter, 0)

	// atomically increment remote counter with the difference value
	err := cs.remoteCounterStorage.Add(ctx, key, old, value.ttl)
	if err != nil {
		cs.logger.Error(err)
		return
	}

	// fetch remote counter value
	remoteCounter, err := cs.remoteCounterStorage.Get(ctx, key)
	if err != nil {
		cs.logger.Error(err)
		return
	}

	// store it on the remoteCounter
	atomic.SwapUint32(&value.remoteCounter, remoteCounter)
}
