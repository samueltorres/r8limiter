package counter

import (
	"context"
	"io/ioutil"
	"sync"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/mock"
)

func TestCounterService_Increment(t *testing.T) {
	testCases := []struct {
		desc         string
		key          string
		ttl          int64
		increment    uint32
		preIncrement bool
		want         uint32
		err          error
	}{
		{
			desc:         "Non Existing key, initializes counter with value",
			key:          "key",
			preIncrement: false,
			increment:    10,
			ttl:          time.Now().Unix() + 30,
			want:         10,
		},
		{
			desc:         "Existing key, increments counter",
			key:          "key",
			preIncrement: true,
			increment:    10,
			ttl:          time.Now().Unix() + 30,
			want:         20,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			counterService := NewCounterService(newInMemStorage(), newNullLogger(), prometheus.NewRegistry(), 100)

			if tC.preIncrement {
				counterService.Increment(context.Background(), tC.key, tC.increment, tC.ttl, -1)
			}

			got, err := counterService.Increment(context.Background(), tC.key, tC.increment, tC.ttl, -1)

			if tC.want != got {
				t.Errorf("got %v, want %v", got, tC.want)
			}

			if tC.err != err {
				t.Errorf("got err %v, want error %v", err, tC.err)
			}
		})
	}
}

func TestCounterService_Increment_ConcurrentIncrement(t *testing.T) {
	// arrange
	key := "key"
	ttl := time.Now().Unix() + 30
	counterService := NewCounterService(newInMemStorage(), newNullLogger(), prometheus.NewRegistry(), 100)

	// act
	wg := sync.WaitGroup{}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				counterService.Increment(context.Background(), key, 1, ttl, -1)
			}
		}()
	}
	wg.Wait()

	got, err := counterService.Get(context.Background(), key)

	// assert
	var want uint32 = 400
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if err != nil {
		t.Errorf("got %v, want no error", err)
	}
}

func TestCounterService_IncrementOnStorage(t *testing.T) {
	testCases := []struct {
		desc       string
		key        string
		increment  uint32
		ttl        int64
		storageRes uint32
		want       uint32
		err        error
	}{
		{
			desc:       "Increments on counter storage",
			key:        "key",
			increment:  2,
			ttl:        time.Now().Unix() + 30,
			storageRes: 22,
			want:       22,
		},
		{
			desc:       "Counter storage fails, returns counter storage error",
			key:        "key",
			increment:  2,
			ttl:        time.Now().Unix() + 30,
			err:        errors.New("error occurred incrementing"),
			storageRes: 0,
			want:       0,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			// arrange
			storageMock := new(storageMock)
			storageMock.On("Increment", mock.Anything, tC.key, tC.increment, tC.ttl).Return(tC.storageRes, tC.err)
			counterService := NewCounterService(storageMock, newNullLogger(), prometheus.NewRegistry(), 100)

			// act
			got, err := counterService.IncrementOnStorage(context.Background(), tC.key, tC.increment, tC.ttl)

			// assert
			if tC.want != got {
				t.Errorf("got %v, want %v", got, tC.want)
			}

			if err != tC.err {
				t.Errorf("got err %v, want err %v", err, tC.err)
			}
		})
	}
}

func TestCounterService_Get(t *testing.T) {
	testCases := []struct {
		desc         string
		key          string
		ttl          int64
		increment    uint32
		preIncrement bool
		err          error
		want         uint32
	}{
		{
			desc:         "Non existing counter",
			key:          "key",
			preIncrement: false,
			err:          ErrNonExistingCounter,
		},
		{
			desc:         "Valid counter",
			key:          "key",
			preIncrement: true,
			increment:    1,
			ttl:          time.Now().Unix() + 60,
			want:         1,
			err:          nil,
		},
	}

	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			// arrange
			counterService := NewCounterService(new(storageMock), newNullLogger(), prometheus.NewRegistry(), 100)

			if tC.preIncrement {
				counterService.Increment(context.Background(), tC.key, tC.increment, tC.ttl, -1)
			}

			// act
			got, err := counterService.Get(context.Background(), tC.key)

			// assert
			if tC.want != got {
				t.Errorf("got %v, want %v", got, tC.want)
			}

			if err != tC.err {
				t.Errorf("got err %v, want err %v", err, tC.err)
			}
		})
	}
}

func TestCounterService_GetFromStorage(t *testing.T) {
	testCases := []struct {
		desc       string
		key        string
		storageRes uint32
		want       uint32
		err        error
	}{
		{
			desc:       "Gets from counter storage",
			key:        "key",
			storageRes: 22,
			want:       22,
		},
		{
			desc:       "Counter storage fails, returns counter storage error",
			key:        "key",
			err:        errors.New("error occurred obtaining counter"),
			storageRes: 0,
			want:       0,
		},
	}
	for _, tC := range testCases {
		t.Run(tC.desc, func(t *testing.T) {
			// arrange
			storageMock := new(storageMock)
			storageMock.On("Get", mock.Anything, tC.key).Return(tC.storageRes, tC.err)
			counterService := NewCounterService(storageMock, newNullLogger(), prometheus.NewRegistry(), 100)

			// act
			got, err := counterService.GetFromStorage(context.Background(), tC.key)

			// assert
			if tC.want != got {
				t.Errorf("got %v, want %v", got, tC.want)
			}

			if err != tC.err {
				t.Errorf("got err %v, want err %v", err, tC.err)
			}
		})
	}
}

func TestCounterService_RunSync_RemovesOutdatedCounters(t *testing.T) {
	// arrange
	key := "key"
	counterService := NewCounterService(newInMemStorage(), newNullLogger(), prometheus.NewRegistry(), 1)

	// act
	cancel := make(chan struct{})
	errch := make(chan error)
	go func() {
		errch <- counterService.RunSync(cancel)
	}()

	counterService.Increment(context.Background(), key, 1, time.Now().Unix()-1, 2)
	time.Sleep(1 * time.Second)
	close(cancel)
	<-errch

	// assert
	_, err := counterService.Get(context.Background(), key)
	if err != ErrNonExistingCounter {
		t.Errorf("got err %v, want error %v", err, ErrNonExistingCounter)
	}
}

func TestCounterService_RunSync_SynchronizesCounters(t *testing.T) {
	// arrange
	key := "key"

	counterService := NewCounterService(newInMemStorage(), newNullLogger(), prometheus.NewRegistry(), 1)

	// act
	cancel := make(chan struct{})
	errch := make(chan error)
	go func() {
		errch <- counterService.RunSync(cancel)
	}()

	counterService.Increment(context.Background(), key, 5, time.Now().Unix()+30, 1)
	time.Sleep(1 * time.Second)
	counterService.Increment(context.Background(), key, 5, time.Now().Unix()+30, 1)
	time.Sleep(2000 * time.Millisecond)

	close(cancel)
	<-errch

	// assert
	var want uint32 = 10
	res, err := counterService.GetFromStorage(context.Background(), key)
	if res != want {
		t.Errorf("got %v, want %v", res, want)
	}

	if err != nil {
		t.Errorf("got err %v, want no error", err)
	}
}

func newNullLogger() *logrus.Logger {
	logger := logrus.New()
	logger.Out = ioutil.Discard

	return logger
}

type storageMock struct {
	mock.Mock
}

func (s *storageMock) BatchIncrement(ctx context.Context, incrs []CounterInc) ([]CounterIncResponse, error) {
	args := s.Called(ctx, incrs)
	return args.Get(0).([]CounterIncResponse), args.Error(1)
}

func (s *storageMock) Increment(ctx context.Context, key string, n uint32, ttl int64) (uint32, error) {
	args := s.Called(ctx, key, n, ttl)
	return args.Get(0).(uint32), args.Error(1)
}

func (s *storageMock) Get(ctx context.Context, key string) (uint32, error) {
	args := s.Called(ctx, key)
	return args.Get(0).(uint32), args.Error(1)
}

type inmemStorage struct {
	mux      sync.Mutex
	counters map[string]uint32
}

func newInMemStorage() *inmemStorage {
	return &inmemStorage{mux: sync.Mutex{}, counters: make(map[string]uint32)}
}

func (s *inmemStorage) BatchIncrement(ctx context.Context, incrs []CounterInc) ([]CounterIncResponse, error) {
	s.mux.Lock()
	s.mux.Unlock()

	resp := make([]CounterIncResponse, len(incrs))
	for i := 0; i < len(incrs); i++ {
		s.counters[incrs[i].Key] += incrs[i].Inc

		resp[i] = CounterIncResponse{}
		resp[i].Key = incrs[i].Key
		resp[i].Curr = s.counters[incrs[i].Key]
	}
	return resp, nil
}

func (s *inmemStorage) Increment(ctx context.Context, key string, n uint32, ttl int64) (uint32, error) {
	s.mux.Lock()
	s.mux.Unlock()

	s.counters[key] += n
	return s.counters[key], nil
}

func (s *inmemStorage) Get(ctx context.Context, key string) (uint32, error) {
	s.mux.Lock()
	s.mux.Unlock()

	if c, ok := s.counters[key]; ok {
		return c, nil
	}

	return 0, errors.New("not existing counter")
}
