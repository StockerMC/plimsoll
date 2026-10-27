package percentile

import (
	"testing"
	"time"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender"
)

var day0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// bucketTop is what Percentile returns for a sample of value v.
func bucketTop(o histogram.HistogramOptions, v float64) float64 {
	return o.GetBucketStart(o.FindBucket(v) + 1)
}

func one(instance string, peak float64) []recommender.InstancePeak {
	return []recommender.InstancePeak{{Instance: instance, Peak: peak}}
}

// Eight daily peaks of 100, 110, ..., 170 MB. Each lands in its own window,
// keyed at the window end, so under the 24 h half-life their weights are
// 1, 2, 4, ..., 128 (sum 255). The p90 threshold 229.5 is only crossed by the
// last sample, 1.7e8, which sits in bucket floor(log1.05(1.7e8*0.05/1e7 + 1))
// = 12. Its upper edge is 1e7 * (1.05^13 - 1) / 0.05 = 177129828.46, and the
// target is that times 1.15 = 203699302.73.
func TestTargetIsP90BucketTopPlusMargin(t *testing.T) {
	r := New(Options{Buckets: memoryOptions, SamplesPerDay: 288})
	for i := 0; i < 8; i++ {
		r.Observe(recommender.Sample{
			Time:  day0.Add(time.Duration(i) * 24 * time.Hour),
			Peaks: one("a", 1e8+float64(i)*1e7),
		})
	}
	inEpsilon(t, r.Recommend().Target, 203699302.73, 1e-9)
}

func TestRecordOOM(t *testing.T) {
	tests := []struct {
		name    string
		peak    float64
		request float64
		want    float64
	}{
		{"ratio wins", 1e8, 1e9, 1.2e9},
		{"minimum bump wins", 1e8, 1e8, 1e8 + 100*1024*1024},
		{"window peak above request", 2e9, 1e9, 2.4e9},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := New(Options{Buckets: memoryOptions, SamplesPerDay: 1440})
			r.Observe(recommender.Sample{Time: day0, Peaks: one("a", tt.peak)})
			if err := r.RecordOOM("a", day0.Add(time.Minute), tt.request); err != nil {
				t.Fatal(err)
			}
			want := bucketTop(memoryOptions, tt.want)
			// One sample in the histogram, so every percentile reads it.
			for _, p := range []float64{0, 0.5, 1} {
				if got := r.peaks.Percentile(p); got != want {
					t.Errorf("Percentile(%v) = %v, want %v", p, got, want)
				}
			}
		})
	}
}

func TestRecordOOMTooOld(t *testing.T) {
	r := New(Options{Buckets: memoryOptions, SamplesPerDay: 1440})
	r.Observe(recommender.Sample{Time: day0, Peaks: one("a", 1e8)})
	if err := r.RecordOOM("a", day0.Add(-25*time.Hour), 1e9); err == nil {
		t.Error("want error for an OOM older than the aggregation interval")
	}
}

// Two tasks alternating between buckets 3 and 5 every window give each task a
// daily peak in bucket 5 and nothing in bucket 3.
func TestPerTaskDailyPeak(t *testing.T) {
	units, err := histogram.NewLinearHistogramOptions(10, 1, 1e-15)
	if err != nil {
		t.Fatal(err)
	}
	r := New(Options{Buckets: units, SamplesPerDay: 288})
	for i := 0; i < 288; i++ {
		a, b := 3.5, 5.5
		if i%2 == 1 {
			a, b = b, a
		}
		r.Observe(recommender.Sample{
			Time:  day0.Add(time.Duration(i) * 5 * time.Minute),
			Peaks: []recommender.InstancePeak{{Instance: "a", Peak: a}, {Instance: "b", Peak: b}},
		})
	}
	for _, p := range []float64{0, 0.5, 1} {
		if got := r.peaks.Percentile(p); got != 6 {
			t.Errorf("Percentile(%v) = %v, want 6 (top of bucket 5)", p, got)
		}
	}
}

func TestReplayConfidenceAfterOneDay(t *testing.T) {
	r := New(Options{Buckets: memoryOptions, SamplesPerDay: 288})
	for i := 0; i < 288; i++ {
		r.Observe(recommender.Sample{
			Time:  day0.Add(time.Duration(i) * 5 * time.Minute),
			Peaks: one("a", 1e8),
		})
	}
	// 288 windows of 288 a day, over a lifespan of 287 windows: 0.9965.
	c := confidence(r.first, r.last, r.samples, r.opts.SamplesPerDay)
	if c < 0.99 || c > 1 {
		t.Errorf("confidence = %v, want about 1.0", c)
	}
}
