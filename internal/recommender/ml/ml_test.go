package ml

import (
	"math"
	"math/rand/v2"
	"slices"
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

// One exponential average per (model, limit), Eq. 4 to 6 as printed from
// zero, divided by the weight the windows so far carry, 1 - (1-d)^t. The
// candidate limits are the distinct bucket edges, so the buckets past a
// candidate's overrun it and the buckets before it are under it.
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
		seen := 1 - math.Pow(1-d, float64(i+1))
		for k := range tops {
			o, u := 0.0, 0.0
			for j := range tops {
				if j > k {
					o += counts[j] / n
				} else if j < k {
					u += counts[j] / n
				}
			}
			over[k] = (1-d)*over[k] + d*o
			under[k] = (1-d)*under[k] + d*u
		}
		best := math.Inf(1)
		next := 0.0
		for k, limit := range tops {
			if k > 0 && limit == tops[k-1] {
				// The last bucket starts at the top of the one before.
				continue
			}
			cost := (weights.Overrun*over[k] + weights.Underrun*under[k]) / seen
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
			if math.Abs(arm.above[k]-over[k]/seen) > 1e-9 || math.Abs(arm.below[k]-under[k]/seen) > 1e-9 {
				t.Fatalf("window %d bucket %d: above %v below %v, want %v %v", i, k, arm.above[k], arm.below[k], over[k]/seen, under[k]/seen)
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
func (f fixed) Raw() float64           { return float64(f) }

// Arm 0 holds 5, arm 1 holds 10. For twenty windows one task sits at 8: arm
// 1 over-provisions (cost 1), arm 0 overruns (cost 5). Then four tasks sit
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
func (s *script) Raw() float64   { return s.limits[s.i] }

// The inner arm's raw limit is 4 before the first window and 8 after it.
// One task at 7.5 (bucket top 8) records ratio 2 against the 4 held before
// the window, not 1 against the 8 fitted on it, and the limit is 8 x 2. The
// next window's task at 3.5 records 0.5 against the 8 and leaves the first
// ratio as it was.
func TestRatioDividesPeakByRawHeldBeforeWindow(t *testing.T) {
	buckets := unitBuckets(t, 20)
	r := NewRatio(buckets, &script{limits: []float64{4, 8, 8}}, 10, 1).(*ratio)
	for i, tt := range []struct {
		peak  float64
		ring  []float64
		limit float64
	}{{7.5, []float64{2}, 16}, {3.5, []float64{2, 0.5}, 16}} {
		r.Add(recommender.Sample{Peaks: []recommender.InstancePeak{{Instance: "a", Peak: tt.peak}}}.Counts(buckets), t0.Add(time.Duration(i)*step))
		if !slices.Equal(r.ring, tt.ring) || r.Limit() != tt.limit {
			t.Errorf("window %d: ring %v limit %v, want %v %v", i, r.ring, r.Limit(), tt.ring, tt.limit)
		}
	}
}

// Over a constant raw limit of 10 and random peaks, the wrapped limit is 10
// times the largest of the last five ratios, or 10 times the floor 1.2 when
// every one of them is below it.
func TestRatioLimitIsRawTimesLargestRecentRatio(t *testing.T) {
	buckets := unitBuckets(t, 30)
	const windows = 5
	r := NewRatio(buckets, fixed(10), windows, 1.2)
	rng := rand.New(rand.NewPCG(3, 4))
	var ratios []float64
	for i := 0; i < 200; i++ {
		top := float64(1 + rng.IntN(20))
		r.Add(recommender.Sample{Peaks: []recommender.InstancePeak{{Instance: "a", Peak: top - 0.5}}}.Counts(buckets), t0.Add(time.Duration(i)*step))
		ratios = append(ratios, top/10)
		want := 10 * math.Max(1.2, slices.Max(ratios[max(0, len(ratios)-windows):]))
		if got := r.Limit(); math.Abs(got-want) > 1e-9 {
			t.Fatalf("window %d: limit %v, want %v", i, got, want)
		}
	}
}

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

// With a one-window ring the recommendation is the arm's limit. A fall of
// more than 10% is followed on the third window in a row and a rise at
// once; a fall under 10% is never followed, and a window back within 10%
// restarts the count.
func TestFallHysteresis(t *testing.T) {
	tests := []struct {
		name   string
		limits []float64
		want   []float64
	}{
		{"falls late, rises at once", []float64{0, 10, 5, 5, 5, 5, 20, 19}, []float64{10, 10, 10, 5, 5, 20, 20, 20, 20, 20}},
		{"interrupted fall restarts", []float64{0, 10, 5, 5, 9.5, 5, 5, 5}, []float64{10, 10, 10, 10, 10, 10, 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(Options{Buckets: unitBuckets(t, 30), Arms: []Arm{&script{limits: tt.limits}},
				Decay: 0.2, MaxWindows: 1, FallFraction: 0.1, FallWindows: 3})
			at := t0
			for i, want := range tt.want {
				s.Observe(recommender.Sample{Time: at, Peaks: []recommender.InstancePeak{{Instance: "a", Peak: 0.5}}})
				at = at.Add(step)
				if got := s.Recommend().Target; got != want {
					t.Errorf("window %d: Recommend = %v, want %v", i, got, want)
				}
			}
		})
	}
}

// The arm holds 5 before the window and 10 after it. One task at 7.5 overruns
// 5 (cost 5) and is under 10 (cost 1); the cost charged must be the first,
// or the arm is scored on a fit to the window it is scored against.
func TestSelectorScoresLimitHeldBeforeWindow(t *testing.T) {
	s := New(Options{Buckets: unitBuckets(t, 20), Arms: []Arm{&script{limits: []float64{5, 10}}},
		Weights: Weights{Overrun: 5, Underrun: 1}, Decay: 1})
	s.Observe(recommender.Sample{Time: t0, Peaks: []recommender.InstancePeak{{Instance: "a", Peak: 7.5}}})
	if s.costs[0] != 5 {
		t.Errorf("cost %v, want 5 for the limit held before the window", s.costs[0])
	}
}

// For ColdWindows windows the recommendation is the largest peak seen, its
// bucket top 8 with a 25% margin, then the selector's choice.
func TestColdStartUsesLargestPeakWithMargin(t *testing.T) {
	s := New(Options{Buckets: unitBuckets(t, 20), Arms: []Arm{fixed(3)}, Decay: 0.2, MaxWindows: 1, ColdWindows: 3,
		Margin: func(float64) float64 { return 0.25 }})
	at := t0
	for i, want := range []float64{10, 10, 10, 3} {
		s.Observe(recommender.Sample{Time: at, Peaks: []recommender.InstancePeak{{Instance: "a", Peak: 7.5}}})
		at = at.Add(step)
		if got := s.Recommend().Target; got != want {
			t.Errorf("window %d: Recommend = %v, want %v", i+1, got, want)
		}
	}
}
