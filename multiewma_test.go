package metrics

import "testing"

func BenchmarkMultiEWMA(b *testing.B) {
	a := NewMultiEWMA()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		a.Update(1)
		a.Tick()
	}
}

func TestMultiEWMA(t *testing.T) {
	a := NewMultiEWMA()
	a.Update(3)
	a.Tick()
	for minute, _ := range ewmaRates1 {
		if rate := a.Rate1(); ewmaRates1[minute] != rate {
			t.Errorf("%v minute a.Rate(): %v != %v\n", minute, ewmaRates1[minute], rate)
		}
		if rate := a.Rate5(); ewmaRates5[minute] != rate {
			t.Errorf("%v minute a.Rate(): %v != %v\n", minute, ewmaRates5[minute], rate)
		}
		if rate := a.Rate15(); ewmaRates15[minute] != rate {
			t.Errorf("%v minute a.Rate(): %v != %v\n", minute, ewmaRates15[minute], rate)
		}
		elapseMultiEWMAMinute(a)
	}
}

func elapseMultiEWMAMinute(a MultiEWMA) {
	for i := 0; i < 12; i++ {
		a.Tick()
	}
}
