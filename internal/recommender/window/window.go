package window

import (
	"math"
	"time"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender"
)

// Stat picks the memory statistic.
type Stat int

const (
	// Max is the peak over the last PeakWindows windows.
	Max Stat = iota
	// Avg is the decayed mean of the per-window mean usage.
	Avg
	// Percentile is the Percentile of load-adjusted usage: each bucket is
	// weighted by its decayed task count times its usage.
	Percentile
	// Intermediate is the larger of the 60th percentile and half the peak.
	Intermediate
)

// Options configure a Recommender.
type Options struct {
	Buckets  histogram.HistogramOptions
	HalfLife time.Duration
	Stat     Stat
	// Percentile in (0, 1], used by the Percentile statistic.
	Percentile float64
	// Windows the peak statistic looks back over.
	PeakWindows int
	// Margin returns the safety margin, as a fraction, to add to a raw
	// statistic. Nil adds none.
	Margin func(raw float64) float64
	// Windows over which the recommendation is the max of recent limits.
	MaxWindows int
	// Floor on every recommendation.
	MinMemory float64
}

// Recommender is one moving-window statistic with its margin and the max
// over the last hour.
type Recommender struct {
	opts  Options
	tops  []float64
	usage []float64 // decayed task count per bucket
	wsum  float64   // decayed window count
	mean  float64   // decayed sum of per-window mean usage
	peaks []float64 // last PeakWindows window peaks
	last  time.Time
	raw   float64   // latest statistic
	limit float64   // latest margin-inclusive statistic
	ring  []float64 // last MaxWindows limits
}

var _ recommender.Recommender = (*Recommender)(nil)

// New returns an empty Recommender.
func New(opts Options) *Recommender {
	return &Recommender{
		opts:  opts,
		tops:  recommender.BucketTops(opts.Buckets),
		usage: make([]float64, opts.Buckets.NumBuckets()),
	}
}

// Observe bins the sample's peaks and adds them as one window.
func (r *Recommender) Observe(s recommender.Sample) {
	if len(s.Peaks) == 0 {
		return
	}
	r.Add(s.Counts(r.opts.Buckets), s.Time)
}

// Add folds one window's task count per bucket into the statistics.
func (r *Recommender) Add(counts []float64, at time.Time) {
	n := sum(counts)
	if n == 0 {
		return
	}
	// Samples lose half their weight every half-life, however far apart
	// the windows are.
	a := 0.0
	if !r.last.IsZero() {
		a = math.Exp2(-float64(at.Sub(r.last)) / float64(r.opts.HalfLife))
		if a > 1 {
			a = 1
		}
	}
	r.last = at
	peak := 0.0
	load := 0.0
	for k, c := range counts {
		r.usage[k] = a*r.usage[k] + c
		load += r.tops[k] * c
		if c > 0 {
			peak = r.tops[k]
		}
	}
	r.wsum = a*r.wsum + 1
	r.mean = a*r.mean + load/n
	r.peaks = push(r.peaks, peak, r.opts.PeakWindows)

	r.raw = r.stat()
	r.limit = r.raw
	if r.opts.Margin != nil {
		r.limit = r.raw * (1 + r.opts.Margin(r.raw))
	}
	r.ring = push(r.ring, r.limit, r.opts.MaxWindows)
}

func (r *Recommender) stat() float64 {
	switch r.opts.Stat {
	case Max:
		return maxOf(r.peaks)
	case Avg:
		return r.mean / r.wsum
	case Percentile:
		return r.percentile(r.opts.Percentile)
	case Intermediate:
		return math.Max(r.percentile(0.6), 0.5*maxOf(r.peaks))
	}
	panic("unknown statistic")
}

// percentile reads the first bucket at which the cumulative load, task
// count times usage, reaches p of the total (Fig. 2 of the paper). Nineteen
// tasks at usage 1 and one at usage 10 put the 90th percentile at 10, where
// a percentile of tasks would say 1.
func (r *Recommender) percentile(p float64) float64 {
	total := 0.0
	for k, w := range r.usage {
		total += r.tops[k] * w
	}
	if total == 0 {
		return 0
	}
	partial := 0.0
	for k, w := range r.usage {
		partial += r.tops[k] * w
		if partial >= p*total {
			return r.tops[k]
		}
	}
	return r.tops[len(r.tops)-1]
}

// Limit is the latest margin-inclusive statistic, before the max over the
// last hour.
func (r *Recommender) Limit() float64 {
	return r.limit
}

// Raw is the latest statistic before its margin.
func (r *Recommender) Raw() float64 {
	return r.raw
}

// Recommend returns the max of the last MaxWindows limits, floored at
// MinMemory, as target and both bounds.
func (r *Recommender) Recommend() recommender.Recommendation {
	v := math.Max(maxOf(r.ring), r.opts.MinMemory)
	return recommender.Recommendation{Target: v, Lower: v, Upper: v}
}

// RecordOOM does nothing: the window statistics have no OOM feedback in the
// paper.
func (r *Recommender) RecordOOM(string, time.Time, float64) error {
	return nil
}

// StepMargin is the paper's "10 to 15%, less for large limits" as two
// steps: 15% below large, 10% at or above it.
func StepMargin(large float64) func(float64) float64 {
	return func(raw float64) float64 {
		if raw < large {
			return 0.15
		}
		return 0.10
	}
}

func push(ring []float64, v float64, n int) []float64 {
	ring = append(ring, v)
	if len(ring) > max(n, 1) {
		ring = ring[1:]
	}
	return ring
}

func sum(xs []float64) float64 {
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s
}

func maxOf(xs []float64) float64 {
	m := 0.0
	for _, x := range xs {
		m = math.Max(m, x)
	}
	return m
}
