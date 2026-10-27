package percentile

import (
	"math"
	"time"

	"github.com/StockerMC/plimsoll/internal/histogram"
)

// Defaults from upstream model/aggregations_config.go.
const (
	defaultAggregationInterval = 24 * time.Hour
	defaultHalfLife            = 24 * time.Hour
	oomBumpUpRatio             = 1.2
	oomMinBumpUp               = 100 * 1024 * 1024
)

// instance keeps one peak per aggregation interval for one task, the port of
// upstream's ContainerState memory fields.
type instance struct {
	peak       float64
	oomPeak    float64
	windowEnd  time.Time
	lastSample time.Time
}

// add puts value into the current window, replacing the window's peak in the
// histogram if value is larger, or opens the window that holds at.
func (s *instance) add(h histogram.Histogram, value float64, at time.Time, oom bool, interval time.Duration) bool {
	if value < 0 || (!oom && at.Before(s.lastSample)) {
		return false
	}
	s.lastSample = at
	if s.windowEnd.IsZero() {
		s.windowEnd = at
	}
	addPeak := false
	if at.Before(s.windowEnd) {
		old := math.Max(s.peak, s.oomPeak)
		if old != 0 && value > old {
			h.SubtractSample(old, 1.0, s.windowEnd)
			addPeak = true
		}
	} else {
		shift := at.Sub(s.windowEnd).Truncate(interval) + interval
		s.windowEnd = s.windowEnd.Add(shift)
		s.peak = 0
		s.oomPeak = 0
		addPeak = true
	}
	if addPeak {
		h.AddSample(value, 1.0, s.windowEnd)
		if oom {
			s.oomPeak = value
		} else {
			s.peak = value
		}
	}
	return true
}
