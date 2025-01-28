package metrics

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// EWMAs continuously calculate an exponentially-weighted moving average
// based on an outside source of clock ticks.
type EWMA interface {
	Rate() float64
	Snapshot() EWMA
	Tick()
	Update(int64)
}

// NewEWMA constructs a new EWMA with the given alpha.
func NewEWMA(alpha float64) EWMA {
	if UseNilMetrics {
		return NilEWMA{}
	}
	return &StandardEWMA{alpha: alpha}
}

const (
	oneMinuteDecay     = -5.0 / 60.0 / 1
	fiveMinuteDecay    = -5.0 / 60.0 / 5
	fifteenMinuteDecay = -5.0 / 60.0 / 15
)

// NewEWMA1 constructs a new EWMA for a one-minute moving average.
func NewEWMA1() EWMA {
	return NewEWMA(1 - math.Exp(oneMinuteDecay))
}

// NewEWMA5 constructs a new EWMA for a five-minute moving average.
func NewEWMA5() EWMA {
	return NewEWMA(1 - math.Exp(fiveMinuteDecay))
}

// NewEWMA15 constructs a new EWMA for a fifteen-minute moving average.
func NewEWMA15() EWMA {
	return NewEWMA(1 - math.Exp(fifteenMinuteDecay))
}

// EWMASnapshot is a read-only copy of another EWMA.
type EWMASnapshot float64

// Rate returns the rate of events per second at the time the snapshot was
// taken.
func (a EWMASnapshot) Rate() float64 { return float64(a) }

// Snapshot returns the snapshot.
func (a EWMASnapshot) Snapshot() EWMA { return a }

// Tick panics.
func (EWMASnapshot) Tick() {
	panic("Tick called on an EWMASnapshot")
}

// Update panics.
func (EWMASnapshot) Update(int64) {
	panic("Update called on an EWMASnapshot")
}

// NilEWMA is a no-op EWMA.
type NilEWMA struct{}

// Rate is a no-op.
func (NilEWMA) Rate() float64 { return 0.0 }

// Snapshot is a no-op.
func (NilEWMA) Snapshot() EWMA { return NilEWMA{} }

// Tick is a no-op.
func (NilEWMA) Tick() {}

// Update is a no-op.
func (NilEWMA) Update(n int64) {}

// StandardEWMA is the standard implementation of an EWMA and tracks the number
// of uncounted events and processes them on each tick.  It uses the
// sync/atomic package to manage uncounted events.
type StandardEWMA struct {
	uncounted int64 // /!\ this should be the first member to ensure 64-bit alignment
	alpha     float64
	rate      float64
	init      bool
	mutex     sync.Mutex
}

// Rate returns the moving average rate of events per second.
func (a *StandardEWMA) Rate() float64 {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.rate * float64(time.Second)
}

// Snapshot returns a read-only copy of the EWMA.
func (a *StandardEWMA) Snapshot() EWMA {
	return EWMASnapshot(a.Rate())
}

// Tick ticks the clock to update the moving average.  It assumes it is called
// every five seconds.
func (a *StandardEWMA) Tick() {
	instantRate := tickEWMA(&a.uncounted)
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if a.init {
		a.rate = updateEWMARate(a.rate, a.alpha, instantRate)
	} else {
		a.init = true
		a.rate = instantRate
	}
}

// Update adds n uncounted events.
func (a *StandardEWMA) Update(n int64) {
	atomic.AddInt64(&a.uncounted, n)
}

// Update the moving average by incorporating all uncounted events.
func tickEWMA(uncounted *int64) (instantRate float64) {
	count := atomic.LoadInt64(uncounted)
	atomic.AddInt64(uncounted, -count)
	return float64(count) / float64(5*time.Second)
}

// Calculate a new EWMA rate given a decay value and the current rate.
func updateEWMARate(rate float64, alpha float64, instantRate float64) float64 {
	return rate + alpha*(instantRate-rate)
}
