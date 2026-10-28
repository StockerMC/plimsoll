package replay

import (
	"math"
	"time"

	"github.com/StockerMC/plimsoll/internal/recommender"
)

const (
	naiveWindow = 48 * time.Hour
	naiveMargin = 0.15
)

// Naive is the tracking control: the largest peak of the last 48 h with a
// 15% margin.
type Naive struct {
	peaks []peak
}

type peak struct {
	at    time.Time
	value float64
}

var _ recommender.Recommender = (*Naive)(nil)

// NewNaive returns an empty Naive.
func NewNaive() recommender.Recommender {
	return &Naive{}
}

// Observe keeps the window's largest peak and forgets peaks older than 48 h.
func (n *Naive) Observe(s recommender.Sample) {
	if len(s.Peaks) == 0 {
		return
	}
	top := 0.0
	for _, p := range s.Peaks {
		top = math.Max(top, p.Peak)
	}
	n.peaks = append(n.peaks, peak{s.Time, top})
	for len(n.peaks) > 0 && s.Time.Sub(n.peaks[0].at) >= naiveWindow {
		n.peaks = n.peaks[1:]
	}
}

// RecordOOM does nothing.
func (*Naive) RecordOOM(string, time.Time, float64) error {
	return nil
}

// Recommend returns the running maximum with margin as target and bounds.
func (n *Naive) Recommend() recommender.Recommendation {
	v := 0.0
	for _, p := range n.peaks {
		v = math.Max(v, p.value)
	}
	v *= 1 + naiveMargin
	return recommender.Recommendation{Target: v, Lower: v, Upper: v}
}
