package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// Meters count events to produce exponentially-weighted moving average rates
// at one-, five-, and fifteen-minutes and a mean rate.
type Meter interface {
	Count() int64
	Mark(int64)
	Rate1() float64
	Rate5() float64
	Rate15() float64
	RateMean() float64
	Snapshot() Meter
}

// GetOrRegisterMeter returns an existing Meter or constructs and registers a
// new StandardMeter.
func GetOrRegisterMeter(name string, r Registry) Meter {
	if nil == r {
		r = DefaultRegistry
	}
	return r.GetOrRegister(name, NewMeter).(Meter)
}

// NewMeter constructs a new StandardMeter and launches a goroutine.
func NewMeter() Meter {
	if UseNilMetrics {
		return NilMeter{}
	}
	m := newStandardMeter()
	arbiter.Lock()
	defer arbiter.Unlock()
	arbiter.meters = append(arbiter.meters, m)
	if !arbiter.started {
		arbiter.started = true
		go arbiter.tick()
	}
	return m
}

// NewMeter constructs and registers a new StandardMeter and launches a
// goroutine.
func NewRegisteredMeter(name string, r Registry) Meter {
	c := NewMeter()
	if nil == r {
		r = DefaultRegistry
	}
	r.Register(name, c)
	return c
}

// MeterSnapshot is a read-only copy of another Meter.
type MeterSnapshot struct {
	count                          int64
	rate1, rate5, rate15, rateMean float64
}

// Count returns the count of events at the time the snapshot was taken.
func (m *MeterSnapshot) Count() int64 { return m.count }

// Mark panics.
func (*MeterSnapshot) Mark(n int64) {
	panic("Mark called on a MeterSnapshot")
}

// Rate1 returns the one-minute moving average rate of events per second at the
// time the snapshot was taken.
func (m *MeterSnapshot) Rate1() float64 { return m.rate1 }

// Rate5 returns the five-minute moving average rate of events per second at
// the time the snapshot was taken.
func (m *MeterSnapshot) Rate5() float64 { return m.rate5 }

// Rate15 returns the fifteen-minute moving average rate of events per second
// at the time the snapshot was taken.
func (m *MeterSnapshot) Rate15() float64 { return m.rate15 }

// RateMean returns the meter's mean rate of events per second at the time the
// snapshot was taken.
func (m *MeterSnapshot) RateMean() float64 { return m.rateMean }

// Snapshot returns the snapshot.
func (m *MeterSnapshot) Snapshot() Meter { return m }

// NilMeter is a no-op Meter.
type NilMeter struct{}

// Count is a no-op.
func (NilMeter) Count() int64 { return 0 }

// Mark is a no-op.
func (NilMeter) Mark(n int64) {}

// Rate1 is a no-op.
func (NilMeter) Rate1() float64 { return 0.0 }

// Rate5 is a no-op.
func (NilMeter) Rate5() float64 { return 0.0 }

// Rate15is a no-op.
func (NilMeter) Rate15() float64 { return 0.0 }

// RateMean is a no-op.
func (NilMeter) RateMean() float64 { return 0.0 }

// Snapshot is a no-op.
func (NilMeter) Snapshot() Meter { return NilMeter{} }

// StandardMeter is the standard implementation of a Meter.
type StandardMeter struct {
	lock      sync.RWMutex // The lock applies to the snapshot only
	snapshot  *MeterSnapshot
	count     int64
	a         MultiEWMA
	startTime time.Time
}

func newStandardMeter() *StandardMeter {
	return &StandardMeter{
		snapshot:  &MeterSnapshot{},
		a:         NewMultiEWMA(),
		startTime: time.Now(),
	}
}

// Count returns the number of events recorded.
func (m *StandardMeter) Count() int64 {
	return atomic.LoadInt64(&m.count)
}

// Mark records the occurance of n events.
func (m *StandardMeter) Mark(n int64) {
	atomic.AddInt64(&m.count, n)
	m.a.Update(n)
}

// Rate1 returns the one-minute moving average rate of events per second.
func (m *StandardMeter) Rate1() (rate1 float64) {
	m.withUpdatedSnapshot(func(snapshot *MeterSnapshot) {
		rate1 = snapshot.rate1
	})
	return
}

// Rate5 returns the five-minute moving average rate of events per second.
func (m *StandardMeter) Rate5() (rate5 float64) {
	m.withUpdatedSnapshot(func(snapshot *MeterSnapshot) {
		rate5 = snapshot.rate5
	})
	return
}

// Rate15 returns the fifteen-minute moving average rate of events per second.
func (m *StandardMeter) Rate15() (rate15 float64) {
	m.withUpdatedSnapshot(func(snapshot *MeterSnapshot) {
		rate15 = snapshot.rate15
	})
	return
}

// RateMean returns the meter's mean rate of events per second.
func (m *StandardMeter) RateMean() (rateMean float64) {
	m.withUpdatedSnapshot(func(snapshot *MeterSnapshot) {
		rateMean = snapshot.rateMean
	})
	return
}

// Snapshot returns a read-only copy of the meter.
func (m *StandardMeter) Snapshot() (s Meter) {
	m.withUpdatedSnapshot(func(snapshot *MeterSnapshot) {
		s = snapshot
	})
	return
}

// Runs the given function with at least a read lock on the snapshot argument.
func (m *StandardMeter) withUpdatedSnapshot(f func(*MeterSnapshot)) {
	// avoid exclusive access if possible
	m.lock.RLock()
	if atomic.LoadInt64(&m.count) == m.snapshot.count {
		f(m.snapshot)
		m.lock.RUnlock()
		return
	}
	m.lock.RUnlock()
	m.lock.Lock()
	defer m.lock.Unlock()
	if atomic.LoadInt64(&m.count) != m.snapshot.count {
		m.updateSnapshot()
	}
	f(m.snapshot)
}

func (m *StandardMeter) updateSnapshot() {
	// should run with write lock held on m.lock
	snapshot := m.snapshot
	snapshot.count = atomic.LoadInt64(&m.count)
	snapshot.rate1 = m.a.Rate1()
	snapshot.rate5 = m.a.Rate5()
	snapshot.rate15 = m.a.Rate15()
	snapshot.rateMean = float64(snapshot.count) / time.Since(m.startTime).Seconds()
}

func (m *StandardMeter) tick() {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.a.Tick()
	m.updateSnapshot()
}

type meterArbiter struct {
	sync.RWMutex
	started bool
	meters  []*StandardMeter
	ticker  *time.Ticker
}

var arbiter = meterArbiter{ticker: time.NewTicker(5e9)}

// Ticks meters on the scheduled interval
func (ma *meterArbiter) tick() {
	for {
		select {
		case <-ma.ticker.C:
			ma.tickMeters()
		}
	}
}

func (ma *meterArbiter) tickMeters() {
	ma.RLock()
	defer ma.RUnlock()
	for _, meter := range ma.meters {
		meter.tick()
	}
}
