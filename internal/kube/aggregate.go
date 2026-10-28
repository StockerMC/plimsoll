package kube

import (
	"slices"
	"strings"
	"time"

	"github.com/StockerMC/plimsoll/internal/recommender"
)

// Aggregator reduces metrics-server point samples to one peak per pod per
// window, the Sample the recommenders consume. Windows are aligned to
// multiples of the interval and keyed by their start.
type Aggregator struct {
	interval time.Duration
	start    time.Time
	peaks    map[string]float64
}

func NewAggregator(interval time.Duration) *Aggregator {
	return &Aggregator{interval: interval, peaks: make(map[string]float64)}
}

// AddAll records one pass's readings in time order, so the windows do not
// depend on the order the pods were listed in, and returns the windows closed.
func (a *Aggregator) AddAll(readings []MemoryUsage) []recommender.Sample {
	sorted := slices.Clone(readings)
	slices.SortStableFunc(sorted, func(x, y MemoryUsage) int { return x.Time.Compare(y.Time) })
	var closed []recommender.Sample
	for _, r := range sorted {
		if s, ok := a.Add(r.Pod, r.Bytes, r.Time); ok {
			closed = append(closed, s)
		}
	}
	return closed
}

// Add records one point sample. When at falls in a later window than the
// open one, the open window is closed and returned. A sample from a window
// already closed is dropped, as upstream's addMemorySample drops it.
func (a *Aggregator) Add(pod string, bytes float64, at time.Time) (recommender.Sample, bool) {
	start := at.Truncate(a.interval)
	if start.Before(a.start) {
		return recommender.Sample{}, false
	}
	var closed recommender.Sample
	ok := false
	if start.After(a.start) {
		closed, ok = a.close()
		a.start = start
	}
	if bytes > a.peaks[pod] {
		a.peaks[pod] = bytes
	}
	return closed, ok
}

func (a *Aggregator) close() (recommender.Sample, bool) {
	if len(a.peaks) == 0 {
		return recommender.Sample{}, false
	}
	s := recommender.Sample{Time: a.start}
	for pod, peak := range a.peaks {
		s.Peaks = append(s.Peaks, recommender.InstancePeak{Instance: pod, Peak: peak})
	}
	slices.SortFunc(s.Peaks, func(x, y recommender.InstancePeak) int { return strings.Compare(x.Instance, y.Instance) })
	clear(a.peaks)
	return s, true
}
