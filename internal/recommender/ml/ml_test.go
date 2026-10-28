package ml

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender"
)

var (
	t0      = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	step    = 5 * time.Minute
	weights = Weights{Overrun: 5, Underrun: 1, LimitChange: 0.1, ModelChange: 0.1}
)

// Unit buckets: a peak of k-0.5 lands in bucket k-1, whose top is k.
func unitBuckets(t testing.TB, n int) histogram.HistogramOptions {
	o, err := histogram.NewLinearHistogramOptions(float64(n-1), 1, 1e-15)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func randomCounts(r *rand.Rand, n int) []float64 {
	counts := make([]float64, n)
	for i := 0; i < 1+r.IntN(10); i++ {
		counts[r.IntN(n)]++
	}
	return counts
}

// One exponential average per (model, limit), Eq. 4 to 6 as printed, with
// the first window taken whole like the arm does. The candidate limits are
// the distinct bucket edges, so the buckets past a candidate's overrun it
// and the buckets before it are under it.
func TestArgminMatchesOneAveragePerLimit(t *testing.T) {
	buckets := unitBuckets(t, 30)
	tops := recommender.BucketTops(buckets)
	halfLife := 2 * time.Hour
	d := 1 - math.Exp2(-float64(step)/float64(halfLife))
	arm := NewArgmin(buckets, halfLife, 0, weights).(*argmin)
	over := make([]float64, len(tops))
	under := make([]float64, len(tops))
	raw := 0.0
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < 100; i++ {
		counts := randomCounts(r, len(tops))
		n := 0.0
		for _, c := range counts {
			n += c
		}
		dt := d
		if i == 0 {
			dt = 1
		}
		for k := range tops {
			o, u := 0.0, 0.0
			for j := range tops {
				if j > k {
					o += counts[j] / n
				} else if j < k {
					u += counts[j] / n
				}
			}
			over[k] = (1-dt)*over[k] + dt*o
			under[k] = (1-dt)*under[k] + dt*u
		}
		best := math.Inf(1)
		next := 0.0
		for k, limit := range tops {
			if k > 0 && limit == tops[k-1] {
				// The last bucket starts at the top of the one before.
				continue
			}
			cost := weights.Overrun*over[k] + weights.Underrun*under[k]
			if limit != raw {
				cost += weights.LimitChange
			}
			if cost < best {
				best, next = cost, limit
			}
		}
		raw = next

		arm.Add(counts, t0.Add(time.Duration(i)*step))
		for k := range tops {
			if math.Abs(arm.above[k]-over[k]) > 1e-9 || math.Abs(arm.below[k]-under[k]) > 1e-9 {
				t.Fatalf("window %d bucket %d: above %v below %v, want %v %v", i, k, arm.above[k], arm.below[k], over[k], under[k])
			}
		}
		if arm.raw != raw {
			t.Fatalf("window %d: raw limit %v, want %v", i, arm.raw, raw)
		}
	}
}

// fixed is an arm with a constant limit.
type fixed float64

func (fixed) Add([]float64, time.Time) {}
func (f fixed) Limit() float64         { return float64(f) }

// Arm 0 holds 5, arm 1 holds 10. For twenty windows one task sits at 8: arm
// 1 under-provisions (cost 1), arm 0 overruns (cost 5). Then four tasks sit
// at 3 and one at 5: arm 1 still costs 1, arm 0 costs 0.8. The smoothed
// costs cross once; the model-change penalty delays the switch. The
// incumbent is the second arm so the penalty must be charged against the
// previous choice, not the running best.
func TestSelectorSwitchesOnceAndLater(t *testing.T) {
	buckets := unitBuckets(t, 20)
	run := func(modelChange float64) (switches, first int) {
		s := New(Options{
			Buckets: buckets,
			Arms:    []Arm{fixed(5), fixed(10)},
			Weights: Weights{Overrun: 5, Underrun: 1, ModelChange: modelChange},
			Decay:   0.2,
		})
		first = -1
		at := t0
		for i := 0; i < 60; i++ {
			peaks := []recommender.InstancePeak{{Instance: "a", Peak: 7.5}}
			if i >= 20 {
				peaks = []recommender.InstancePeak{{Instance: "a", Peak: 4.5}, {Instance: "b", Peak: 2.5},
					{Instance: "c", Peak: 2.5}, {Instance: "d", Peak: 2.5}, {Instance: "e", Peak: 2.5}}
			}
			was := s.chosen
			s.Observe(recommender.Sample{Time: at, Peaks: peaks})
			at = at.Add(step)
			if was >= 0 && s.chosen != was {
				switches++
				if first < 0 {
					first = i
				}
			}
		}
		if s.Limit() != 5 {
			t.Errorf("model change %v: final limit %v, want 5", modelChange, s.Limit())
		}
		return switches, first
	}
	switches, plain := run(0)
	if switches != 1 || plain < 20 {
		t.Errorf("without penalty: %d switches, first at window %d", switches, plain)
	}
	switches, penalized := run(0.1)
	if switches != 1 || penalized <= plain {
		t.Errorf("with penalty: %d switches, first at window %d, want one after %d", switches, penalized, plain)
	}
}

func argminGrid(buckets histogram.HistogramOptions) []Arm {
	var arms []Arm
	for _, hl := range []time.Duration{6 * time.Hour, 12 * time.Hour, 24 * time.Hour, 48 * time.Hour, 7 * 24 * time.Hour} {
		for _, m := range []float64{0, 0.05, 0.1, 0.15, 0.25} {
			arms = append(arms, NewArgmin(buckets, hl, m, weights))
		}
	}
	return arms
}

func newSelector(buckets histogram.HistogramOptions, arms []Arm) *Selector {
	return New(Options{Buckets: buckets, Arms: arms, Weights: weights, Decay: 0.2, MaxWindows: 12, ColdWindows: 12})
}

// Two selectors see the same windows until window 30 and different ones
// after. The recommendation read before each window is the same on both
// sides through window 30, so no window influences the limit scored
// against it.
func TestRecommendationPrecedesWindow(t *testing.T) {
	buckets := unitBuckets(t, 20)
	a := newSelector(buckets, argminGrid(buckets))
	b := newSelector(buckets, argminGrid(buckets))
	diverged := false
	at := t0
	for i := 0; i < 60; i++ {
		before := a.Recommend()
		if b.Recommend() != before {
			t.Fatalf("window %d: recommendations differ before the window is seen", i)
		}
		usageB := 7.5
		if i >= 30 {
			usageB = 15.5
		}
		a.Observe(recommender.Sample{Time: at, Peaks: []recommender.InstancePeak{{Instance: "a", Peak: 7.5}}})
		b.Observe(recommender.Sample{Time: at, Peaks: []recommender.InstancePeak{{Instance: "a", Peak: usageB}}})
		at = at.Add(step)
		if i >= 30 && a.Recommend() != b.Recommend() {
			diverged = true
			break
		}
	}
	if !diverged {
		t.Error("the two series never diverged, so the test checked nothing")
	}
}

func BenchmarkSelector(b *testing.B) {
	buckets, err := histogram.NewExponentialHistogramOptions(1, math.Exp2(-17), 1.05, 1e-15)
	if err != nil {
		b.Fatal(err)
	}
	const windows = 8928
	r := rand.New(rand.NewPCG(1, 2))
	samples := make([]recommender.Sample, windows)
	for i := range samples {
		samples[i].Time = t0.Add(time.Duration(i) * step)
		for j := 0; j < 10; j++ {
			samples[i].Peaks = append(samples[i].Peaks, recommender.InstancePeak{Instance: string(rune('a' + j)), Peak: 0.1 + 0.05*r.Float64()})
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := newSelector(buckets, append(argminGrid(buckets), fixed(0.2)))
		for _, sample := range samples {
			s.Observe(sample)
		}
	}
}

// script is an arm whose limit follows a list, one entry per window after
// the first, repeating the last entry.
type script struct {
	limits []float64
	i      int
}

func (s *script) Add([]float64, time.Time) {
	if s.i < len(s.limits)-1 {
		s.i++
	}
}

func (s *script) Limit() float64 { return s.limits[s.i] }

// An OOM at request 8 with the instance's last peak at 10 bumps to 12 at
// once and the ring holds it; the next window carries the bump as a peak,
// which lands in the bucket whose top is 13.
func TestRecordOOMRaisesLimitAtOnce(t *testing.T) {
	buckets := unitBuckets(t, 20)
	s := New(Options{Buckets: buckets, Arms: []Arm{fixed(5)}, Decay: 0.2, MaxWindows: 2, OOMMinBump: 1})
	s.Observe(recommender.Sample{Time: t0, Peaks: []recommender.InstancePeak{{Instance: "a", Peak: 10}}})
	if err := s.RecordOOM("a", t0.Add(time.Minute), 8); err != nil {
		t.Fatal(err)
	}
	if got := s.Recommend().Target; got != 12 {
		t.Errorf("after the OOM: Recommend = %v, want 12", got)
	}
	s.Observe(recommender.Sample{Time: t0.Add(step), Peaks: []recommender.InstancePeak{{Instance: "a", Peak: 0.5}}})
	if got := s.Recommend().Target; got != 12 {
		t.Errorf("one window after the OOM: Recommend = %v, want 12 held by the ring", got)
	}
	if got := s.peak; got != 13 {
		t.Errorf("largest peak seen = %v, want 13, the top of the bucket the bump 12 fell in", got)
	}
}

func TestRecommendationHoldsHigherLimitForMaxWindows(t *testing.T) {
	buckets := unitBuckets(t, 20)
	s := New(Options{Buckets: buckets, Arms: []Arm{&script{limits: []float64{0, 10, 1}}}, Decay: 0.2, MaxWindows: 3})
	at := t0
	for i, want := range []float64{10, 10, 10, 1} {
		s.Observe(recommender.Sample{Time: at, Peaks: []recommender.InstancePeak{{Instance: "a", Peak: 0.5}}})
		at = at.Add(step)
		if got := s.Recommend().Target; got != want {
			t.Errorf("window %d: Recommend = %v, want %v", i, got, want)
		}
	}
}
