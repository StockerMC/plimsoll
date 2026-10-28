package window

import (
	"slices"
	"testing"
	"time"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// Unit buckets: a peak of k-0.5 lands in bucket k-1, whose top is k.
func unitBuckets(t *testing.T) histogram.HistogramOptions {
	o, err := histogram.NewLinearHistogramOptions(20, 1, 1e-15)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// window is one window of n tasks at usage u.
func window(n int, u float64) []recommender.InstancePeak {
	peaks := make([]recommender.InstancePeak, n)
	for i := range peaks {
		peaks[i] = recommender.InstancePeak{Instance: string(rune('a' + i)), Peak: u - 0.5}
	}
	return peaks
}

func TestStatistics(t *testing.T) {
	type step struct {
		tasks  int
		usage  float64
		after  time.Duration // gap from the previous window, default 5 min
		limit  float64       // Limit after this window, 0 to skip
		target float64       // Recommend after this window, 0 to skip
	}
	tests := []struct {
		name  string
		opts  Options
		steps []step
	}{
		{
			// Nineteen windows of one task at 1 and one of one task at 10.
			// Load is 19 at usage 1 and 10 at usage 10, so the 90th
			// percentile by load is 10; by task or by window it is 1.
			name:  "percentile is by load, not by task count",
			opts:  Options{Stat: Percentile, Percentile: 0.9, HalfLife: 1000 * time.Hour},
			steps: append(slices.Repeat([]step{{1, 1, 0, 0, 0}}, 18), step{1, 1, 0, 1, 0}, step{1, 10, 0, 10, 0}),
		},
		{
			name: "peak forgets a window after exactly N windows",
			opts: Options{Stat: Max, PeakWindows: 3, HalfLife: time.Hour},
			steps: []step{
				{1, 10, 0, 10, 0}, {1, 1, 0, 10, 0}, {1, 1, 0, 10, 0}, {1, 1, 0, 1, 0},
			},
		},
		{
			name: "recommendation holds a higher limit for exactly MaxWindows windows",
			opts: Options{Stat: Max, PeakWindows: 1, MaxWindows: 3, HalfLife: time.Hour},
			steps: []step{
				{1, 10, 0, 10, 10}, {1, 1, 0, 1, 10}, {1, 1, 0, 1, 10}, {1, 1, 0, 1, 1},
			},
		},
		{
			// A window one half-life old weighs half: (0.5 * 4 + 1) / 1.5.
			name: "average halves a window's weight after one half-life",
			opts: Options{Stat: Avg, HalfLife: time.Hour},
			steps: []step{
				{2, 4, 0, 4, 0}, {2, 1, time.Hour, 2, 0},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.opts.Buckets = unitBuckets(t)
			if tt.opts.PeakWindows == 0 {
				tt.opts.PeakWindows = 1
			}
			if tt.opts.MaxWindows == 0 {
				tt.opts.MaxWindows = 1
			}
			r := New(tt.opts)
			at := t0
			for i, s := range tt.steps {
				if i > 0 {
					at = at.Add(max(s.after, 5*time.Minute))
				}
				r.Observe(recommender.Sample{Time: at, Peaks: window(s.tasks, s.usage)})
				if s.limit != 0 && r.Limit() != s.limit {
					t.Errorf("step %d: Limit = %v, want %v", i, r.Limit(), s.limit)
				}
				if s.target != 0 && r.Recommend().Target != s.target {
					t.Errorf("step %d: Recommend = %v, want %v", i, r.Recommend().Target, s.target)
				}
			}
		})
	}
}
