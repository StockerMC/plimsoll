package ml

import (
	"math"
	"time"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender"
)

// Arm is one model the Selector can pick: it sees every window and always
// has a limit.
type Arm interface {
	// Add folds one window's task count per bucket into the model.
	Add(counts []float64, at time.Time)
	// Limit is the model's current margin-inclusive limit.
	Limit() float64
	// Raw is the limit before the margin.
	Raw() float64
}

// Weights are the cost weights shared by every model and the selector:
// overrun and underrun per task fraction, and the penalties for changing the
// limit and for changing the model.
type Weights struct {
	Overrun     float64
	Underrun    float64
	LimitChange float64
	ModelChange float64
}

// argmin is one arg-min model: a decayed histogram of task fractions read
// at the bucket edge that minimizes the weighted overrun plus underrun.
type argmin struct {
	tops     []float64
	halfLife time.Duration
	margin   float64
	weights  Weights
	usage    []float64 // decayed task fraction per bucket, summed over windows
	weight   float64   // decayed window count, the sum of usage
	above    []float64 // above[k]: fraction of tasks over the edge of bucket k
	below    []float64 // below[k]: fraction of tasks under it
	last     time.Time
	raw      float64
	limit    float64
}

// NewArgmin returns a model whose histogram halves its weight every
// halfLife and whose limit carries the given fractional margin.
func NewArgmin(buckets histogram.HistogramOptions, halfLife time.Duration, margin float64, weights Weights) Arm {
	n := buckets.NumBuckets()
	return &argmin{
		tops:     recommender.BucketTops(buckets),
		halfLife: halfLife,
		margin:   margin,
		weights:  weights,
		usage:    make([]float64, n),
		above:    make([]float64, n),
		below:    make([]float64, n),
	}
}

func (a *argmin) Add(counts []float64, at time.Time) {
	n := sum(counts)
	if n == 0 {
		return
	}
	// Eq. 4 and 5 are exponential averages of the task count above and
	// below each candidate limit. Both are linear in the window histogram,
	// so one decayed histogram serves every candidate through prefix sums.
	// Counts are divided by the task count so the costs are fractions and
	// the limit-change penalty means the same for every job. The sums are
	// divided by the decayed window count, as Eq. 1 does, so the first
	// window weighs like any other instead of standing in for the history.
	// Products are rounded before the sums for the reason in Selector.Add.
	keep := 1.0
	if !a.last.IsZero() {
		keep = math.Min(math.Exp2(-float64(at.Sub(a.last))/float64(a.halfLife)), 1)
	}
	a.last = at
	for k, c := range counts {
		a.usage[k] = float64(keep*a.usage[k]) + c/n
	}
	a.weight = float64(keep*a.weight) + 1
	total := 0.0
	for k := range a.usage {
		a.below[k] = total / a.weight
		total += a.usage[k]
	}
	for k := range a.usage {
		a.above[k] = (total-a.usage[k])/a.weight - a.below[k]
	}
	// Eq. 6, ties to the lowest limit.
	prev := a.raw
	best := math.Inf(1)
	for k, top := range a.tops {
		if k > 0 && top == a.tops[k-1] {
			// The last bucket's top is its start, the same limit as the
			// bucket before it, and the tasks in it overrun that limit.
			continue
		}
		cost := float64(a.weights.Overrun*a.above[k]) + float64(a.weights.Underrun*a.below[k])
		if top != prev {
			cost += a.weights.LimitChange
		}
		if cost < best {
			best = cost
			a.raw = top
		}
	}
	// The paper adds its margin; jobs differ by orders of magnitude, so
	// here it is a fraction of the limit.
	a.limit = a.raw * (1 + a.margin)
}

func (a *argmin) Limit() float64 {
	return a.limit
}

func (a *argmin) Raw() float64 {
	return a.raw
}

func sum(xs []float64) float64 {
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s
}
