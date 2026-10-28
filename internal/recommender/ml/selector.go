package ml

import (
	"math"
	"slices"
	"time"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender"
)

// Options configure a Selector.
type Options struct {
	Buckets histogram.HistogramOptions
	Arms    []Arm
	Weights Weights
	// Decay of each arm's smoothed cost per window, the paper's d.
	Decay float64
	// Windows over which the recommendation is the max of recent limits.
	MaxWindows int
	// Windows before the selector's choice is used; until then the limit
	// is the largest peak seen, with margin.
	ColdWindows int
	// Margin returns the safety margin, as a fraction, used during the
	// cold start. Nil adds none.
	Margin func(raw float64) float64
	// Floor on every recommendation.
	MinMemory float64
	// Floor on the bump an OOM adds to the request, 100 MiB in upstream.
	OOMMinBump float64
}

// Selector runs every arm on every window and recommends the limit of the
// arm whose recent limits would have cost least.
type Selector struct {
	opts    Options
	tops    []float64
	costs   []float64 // smoothed cost per arm
	chosen  int       // arm behind the latest limit, -1 before the first window
	limit   float64   // latest selected limit
	ring    []float64 // last MaxWindows limits
	seen    int
	peak    float64
	peaks   map[string]float64         // each instance's peak in the last window
	pending []recommender.InstancePeak // OOM bumps for the next window
}

var _ recommender.Recommender = (*Selector)(nil)

// New returns a Selector before any window.
func New(opts Options) *Selector {
	return &Selector{
		opts:   opts,
		tops:   recommender.BucketTops(opts.Buckets),
		costs:  make([]float64, len(opts.Arms)),
		chosen: -1,
		peaks:  make(map[string]float64),
	}
}

// Observe scores every arm on the window, lets each arm learn from it, and
// picks the arm for the next window.
func (s *Selector) Observe(sample recommender.Sample) {
	peaks := slices.Concat(sample.Peaks, s.pending)
	s.pending = nil
	if len(peaks) == 0 {
		return
	}
	clear(s.peaks)
	for _, p := range sample.Peaks {
		s.peaks[p.Instance] = p.Peak
	}
	s.Add(recommender.Sample{Time: sample.Time, Peaks: peaks}.Counts(s.opts.Buckets), sample.Time)
}

// Add runs one window of task counts per bucket through the selector.
func (s *Selector) Add(counts []float64, at time.Time) {
	n := sum(counts)
	if n == 0 {
		return
	}
	w := s.opts.Weights
	for i, arm := range s.opts.Arms {
		// Eq. 8 on this window alone, at the limit the arm held for it.
		// The paper scores the limit fitted on the window; scoring the
		// limit set before it is what the replay actuates.
		held := arm.Limit()
		arm.Add(counts, at)
		over, under := split(s.tops, counts, held)
		cost := (w.Overrun*over + w.Underrun*under) / n
		if arm.Limit() != held {
			cost += w.LimitChange
		}
		s.costs[i] = s.opts.Decay*cost + (1-s.opts.Decay)*s.costs[i]
	}
	// Eq. 9. Its last term is printed with the new limit on both sides;
	// it is read as a penalty for an arm whose limit differs from the
	// limit now in force.
	prev := s.chosen
	best := math.Inf(1)
	for i, arm := range s.opts.Arms {
		cost := s.costs[i]
		if i != prev {
			cost += w.ModelChange
		}
		if arm.Limit() != s.limit {
			cost += w.LimitChange
		}
		if cost < best {
			best = cost
			s.chosen = i
		}
	}
	for k, c := range counts {
		if c > 0 {
			s.peak = math.Max(s.peak, s.tops[k])
		}
	}
	s.seen++
	s.limit = s.opts.Arms[s.chosen].Limit()
	if s.seen <= s.opts.ColdWindows {
		// The paper queues a job until its first recommendation; a live
		// container needs one at once, so the largest peak seen stands in.
		s.limit = s.peak
		if s.opts.Margin != nil {
			s.limit *= 1 + s.opts.Margin(s.peak)
		}
	}
	s.ring = append(s.ring, s.limit)
	if len(s.ring) > max(s.opts.MaxWindows, 1) {
		s.ring = s.ring[1:]
	}
}

// Limit is the latest selected limit, before the max over the last hour.
func (s *Selector) Limit() float64 {
	return s.limit
}

// Recommend returns the max of the last MaxWindows limits, floored at
// MinMemory, as target and both bounds.
func (s *Selector) Recommend() recommender.Recommendation {
	v := s.opts.MinMemory
	for _, l := range s.ring {
		v = math.Max(v, l)
	}
	return recommender.Recommendation{Target: v, Lower: v, Upper: v}
}

// RecordOOM raises the limit at once to the bump upstream VPA uses, the
// larger of OOMMinBump and 20% over the request or the instance's last
// peak, and adds that value as one more peak in the next window. One peak
// among hours of decayed history barely moves an arm, so without the
// immediate raise a container could OOM again before any arm reacted.
func (s *Selector) RecordOOM(instance string, at time.Time, request float64) error {
	used := math.Max(request, s.peaks[instance])
	needed := math.Max(used+s.opts.OOMMinBump, used*1.2)
	s.pending = append(s.pending, recommender.InstancePeak{Instance: instance, Peak: needed})
	s.limit = math.Max(s.limit, needed)
	if len(s.ring) == 0 {
		s.ring = append(s.ring, s.limit)
	} else {
		s.ring[len(s.ring)-1] = math.Max(s.ring[len(s.ring)-1], s.limit)
	}
	return nil
}

// split sums the tasks whose bucket lies wholly above and wholly below
// limit; the bucket holding the limit counts in neither.
func split(tops, counts []float64, limit float64) (over, under float64) {
	for k, top := range tops {
		switch {
		case top > limit:
			over += counts[k]
		case top < limit:
			under += counts[k]
		}
	}
	return over, under
}
