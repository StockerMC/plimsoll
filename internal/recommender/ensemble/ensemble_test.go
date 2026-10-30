package ensemble

import (
	"math"
	"testing"
	"time"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender"
	"github.com/StockerMC/plimsoll/internal/recommender/ml"
)

func TestPlateauKeepsMarginAfterFall(t *testing.T) {
	for _, scale := range []struct {
		name                     string
		max, bucket, peak, large float64
	}{
		{"live", 1e12, 1e7, 512 << 20, 1 << 30},
		{"replay", 1, math.Exp2(-17), 0.02, 0.05},
	} {
		for _, model := range []struct {
			name string
			opts Options
		}{
			{"plain", Options{}},
			{"held", Options{FallFraction: 0.05, FallWindows: 288, Weights: ml.Weights{Overrun: 20, Underrun: 1, ModelChange: 0.1}}},
			{"argmin", Options{ArgminOnly: true, Decay: 0.05}},
		} {
			t.Run(scale.name+"/"+model.name, func(t *testing.T) {
				buckets, err := histogram.NewExponentialHistogramOptions(scale.max, scale.bucket, 1.05, 1e-4)
				if err != nil {
					t.Fatal(err)
				}
				opts := model.opts
				opts.Buckets, opts.LargeLimit = buckets, scale.large
				s := New(opts)
				at := time.Unix(0, 0)
				observe := func(peak float64) {
					s.Observe(recommender.Sample{Time: at, Peaks: []recommender.InstancePeak{{Instance: "task", Peak: peak}}})
					at = at.Add(5 * time.Minute)
				}
				for range hourWindows {
					observe(2 * scale.peak)
				}
				high := s.Recommend().Target
				// Outlast the cold peak, max ring and full fall hold on a lower plateau.
				for range 7 * 24 * 12 {
					observe(scale.peak)
				}
				for i := 0; i < hourWindows+opts.FallWindows; i++ {
					observe(scale.peak)
					target := s.Recommend().Target
					if target < 1.05*scale.peak || target >= high {
						t.Fatalf("window %d: target/peak %.6f, want >= 1.05 and below high plateau %.6f", i, target/scale.peak, high/scale.peak)
					}
					if s.Limit() < 1.05*scale.peak {
						t.Fatalf("window %d: selected limit/peak %.6f, want >= 1.05", i, s.Limit()/scale.peak)
					}
				}
				t.Logf("settled target/peak %.6f, selected limit/peak %.6f", s.Recommend().Target/scale.peak, s.Limit()/scale.peak)
			})
		}
	}
}
