package metrics

import (
	"math"
	"sync"
	"sync/atomic"
	"time"
)

// MultiEWMAs continuously calculate an exponentially-weighted moving average
// of several intervals, based on an outside source of clock ticks.
type MultiEWMA interface {
	Rate1() float64
	Rate5() float64
	Rate15() float64
	Snapshot() MultiEWMA
	Tick()
	Update(int64)
}

const (
	multiEWMARate1 = iota
	multiEWMARate5
	multiEWMARate15
)

// NewMultiEWMAWithAlphas constructs a new MultiEWMA with the given alphas.
func NewMultiEWMAWithAlphas(alphas [3]float64) MultiEWMA {
	if UseNilMetrics {
		return NilMultiEWMA{}
	}
	return &StandardMultiEWMA{alphas: alphas}
}

// NewMultiEWMA constructs a new MultiEWMA for a one, five and fifteen-minute
// moving average.
func NewMultiEWMA() MultiEWMA {
	return NewMultiEWMAWithAlphas([3]float64{
		1 - math.Exp(oneMinuteDecay),
		1 - math.Exp(fiveMinuteDecay),
		1 - math.Exp(fifteenMinuteDecay)})
}

// MultiEWMASnapshot is a read-only copy of another MultiEWMA.
type MultiEWMASnapshot [3]float64

// Rate1 returns the rate of events per second in the first interval (by
// default, one-minute) at the time the snapshot was taken.
func (a MultiEWMASnapshot) Rate1() float64 { return float64(a[multiEWMARate1]) }

// Rate5 returns the rate of events per second in the first interval (by
// default, five-minute) at the time the snapshot was taken.
func (a MultiEWMASnapshot) Rate5() float64 { return float64(a[multiEWMARate5]) }

// Rate15 returns the rate of events per second in the first interval (by
// default, fifteen-minute) at the time the snapshot was taken.
func (a MultiEWMASnapshot) Rate15() float64 { return float64(a[multiEWMARate15]) }

// Snapshot returns the snapshot.
func (a MultiEWMASnapshot) Snapshot() MultiEWMA { return a }

// Tick panics.
func (MultiEWMASnapshot) Tick() {
	panic("Tick called on an MultiEWMASnapshot")
}

// Update panics.
func (MultiEWMASnapshot) Update(int64) {
	panic("Update called on an MultiEWMASnapshot")
}

// NilMultiEWMA is a no-op MultiEWMA.
type NilMultiEWMA struct{}

// Rate1 is a no-op.
func (NilMultiEWMA) Rate1() float64 { return 0.0 }

// Rate5 is a no-op.
func (NilMultiEWMA) Rate5() float64 { return 0.0 }

// Rate15 is a no-op.
func (NilMultiEWMA) Rate15() float64 { return 0.0 }

// Snapshot is a no-op.
func (NilMultiEWMA) Snapshot() MultiEWMA { return NilMultiEWMA{} }

// Tick is a no-op.
func (NilMultiEWMA) Tick() {}

// Update is a no-op.
func (NilMultiEWMA) Update(n int64) {}

// StandardMultiEWMA is the standard implementation of an EWMA and tracks the number
// of uncounted events and processes them on each tick.  It uses the
// sync/atomic package to manage uncounted events.
type StandardMultiEWMA struct {
	uncounted int64 // /!\ this should be the first member to ensure 64-bit alignment
	alphas    [3]float64
	rates     [3]float64
	init      bool
	mutex     sync.Mutex
}

// Rate returns a moving average rate of events per second.
// The rate duration is specified by the index.
func (a *StandardMultiEWMA) rateByIndex(index int) float64 {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.rates[index] * float64(time.Second)
}

// Rate1 returns the one-minute moving average rate of events per second.
func (a *StandardMultiEWMA) Rate1() float64 {
	return a.rateByIndex(multiEWMARate1)
}

// Rate5 returns the five-minute moving average rate of events per second.
func (a *StandardMultiEWMA) Rate5() float64 {
	return a.rateByIndex(multiEWMARate5)
}

// Rate15 returns the fifteen-minute moving average rate of events per second.
func (a *StandardMultiEWMA) Rate15() float64 {
	return a.rateByIndex(multiEWMARate15)
}

// Snapshot returns a read-only copy of the EWMA.
func (a *StandardMultiEWMA) Snapshot() MultiEWMA {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return MultiEWMASnapshot(a.rates)
}

// Tick ticks the clock to update the moving average.  It assumes it is called
// every five seconds on a single thread.
func (a *StandardMultiEWMA) Tick() {
	instantRate := tickEWMA(&a.uncounted)
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if a.init {
		for index := range a.rates {
			a.rates[index] = updateEWMARate(a.rates[index], a.alphas[index], instantRate)
		}
	} else {
		a.init = true
		for index, _ := range a.rates {
			a.rates[index] = instantRate
		}
	}
}

// Update adds n uncounted events.
func (a *StandardMultiEWMA) Update(n int64) {
	atomic.AddInt64(&a.uncounted, n)
}
