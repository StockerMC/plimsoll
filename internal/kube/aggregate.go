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

// Add records one point sample. When at falls in a later window than the
// open one, the open window is closed and returned. An older sample counts
// towards the open window.
func (a *Aggregator) Add(pod string, bytes float64, at time.Time) (recommender.Sample, bool) {
	start := at.Truncate(a.interval)
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
