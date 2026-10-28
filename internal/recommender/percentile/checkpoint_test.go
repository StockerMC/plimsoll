package percentile

import (
	"testing"
	"time"

	"github.com/StockerMC/plimsoll/internal/recommender"
)

func daily(r *Recommender, days int) {
	for i := 0; i < days; i++ {
		r.Observe(recommender.Sample{
			Time:  day0.Add(time.Duration(i) * 24 * time.Hour),
			Peaks: one("a", float64(i+1)*1e8),
		})
	}
}

// The last of eight daily peaks is in a window that is still open and has the
// largest weight; the restored target has to keep it (p90 lands on 8e8, not 7e8).
// First, Last and Samples feed the confidence multiplier on the bounds; one
// sample per day keeps all three in play.
func TestCheckpointRoundTrip(t *testing.T) {
	opts := Options{Buckets: memoryOptions, SamplesPerDay: 1}
	saved := New(opts)
	daily(saved, 8)
	c, err := saved.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	restored := New(opts)
	if err := restored.Restore(c); err != nil {
		t.Fatal(err)
	}
	got, want := restored.Recommend(), saved.Recommend()
	inEpsilon(t, got.Target, want.Target, 1e-9)
	inEpsilon(t, got.Lower, want.Lower, 1e-9)
	inEpsilon(t, got.Upper, want.Upper, 1e-9)
}

// Restarting from a checkpoint, once or many times, must learn exactly what
// an uninterrupted recommender learns: a sample after a restore lands in the
// window that was open, replacing its peak rather than adding another.
func TestRestartsDoNotChangeLearning(t *testing.T) {
	opts := Options{Buckets: memoryOptions, SamplesPerDay: 24}
	samples := make([]recommender.Sample, 72)
	for i := range samples {
		samples[i] = recommender.Sample{
			Time:  day0.Add(time.Duration(i) * time.Hour),
			Peaks: append(one("a", float64(1+i%7)*1e8), one("b", float64(1+i%5)*2e8)...),
		}
	}
	uninterrupted := New(opts)
	for _, s := range samples {
		uninterrupted.Observe(s)
	}
	for _, every := range []int{72, 5, 1} {
		r := New(opts)
		for i, s := range samples {
			if i > 0 && i%every == 0 {
				c, err := r.Checkpoint()
				if err != nil {
					t.Fatal(err)
				}
				r = New(opts)
				if err := r.Restore(c); err != nil {
					t.Fatal(err)
				}
			}
			r.Observe(s)
		}
		got, want := r.Recommend(), uninterrupted.Recommend()
		inEpsilon(t, got.Target, want.Target, 1e-9)
		inEpsilon(t, got.Lower, want.Lower, 1e-9)
		inEpsilon(t, got.Upper, want.Upper, 1e-9)
		for _, p := range []float64{0.5, 0.9, 0.99} {
			inEpsilon(t, r.peaks.Percentile(p), uninterrupted.peaks.Percentile(p), 1e-9)
		}
	}
}

func TestRestoreRefusesOtherVersion(t *testing.T) {
	opts := Options{Buckets: memoryOptions, SamplesPerDay: 288}
	saved := New(opts)
	daily(saved, 8)
	c, err := saved.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	other := c
	other.Version = "v3"
	halfLife := opts
	halfLife.HalfLife = 48 * time.Hour
	for _, tt := range []struct {
		name string
		opts Options
		c    recommender.Checkpoint
	}{
		{"other version", opts, other},
		{"other half-life", halfLife, c},
	} {
		r := New(tt.opts)
		if err := r.Restore(tt.c); err == nil {
			t.Errorf("%s: want error", tt.name)
		}
		if !r.peaks.IsEmpty() || r.samples != 0 {
			t.Errorf("%s: refused checkpoint changed the recommender", tt.name)
		}
	}
}
