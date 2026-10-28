package ml

import (
	"math"
	"time"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender"
)

// ratio replaces an arm's fixed margin with one read off its own history:
// the largest ratio of a window's peak to the raw limit the arm held before
// that window, over the last windows windows, at least floor.
type ratio struct {
	inner   Arm
	tops    []float64
	windows int
	floor   float64
	ring    []float64
	limit   float64
}

// NewRatio wraps inner so its limit is its raw limit times the largest
// peak-to-raw ratio of the last windows windows, floored at floor.
func NewRatio(buckets histogram.HistogramOptions, inner Arm, windows int, floor float64) Arm {
	return &ratio{inner: inner, tops: recommender.BucketTops(buckets), windows: windows, floor: floor, limit: inner.Limit()}
}

func (r *ratio) Add(counts []float64, at time.Time) {
	if sum(counts) == 0 {
		return
	}
	raw := r.inner.Raw()
	r.inner.Add(counts, at)
	if raw > 0 {
		peak := 0.0
		for k, c := range counts {
			if c > 0 {
				peak = r.tops[k]
			}
		}
		r.ring = append(r.ring, peak/raw)
		if len(r.ring) > r.windows {
			r.ring = r.ring[1:]
		}
	}
	m := r.floor
	for _, x := range r.ring {
		m = math.Max(m, x)
	}
	r.limit = r.inner.Raw() * m
}

func (r *ratio) Limit() float64 {
	return r.limit
}

func (r *ratio) Raw() float64 {
	return r.inner.Raw()
}
