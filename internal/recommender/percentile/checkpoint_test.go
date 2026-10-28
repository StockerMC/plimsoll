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

func TestRestoreRefusesOtherVersion(t *testing.T) {
	opts := Options{Buckets: memoryOptions, SamplesPerDay: 288}
	saved := New(opts)
	daily(saved, 8)
	c, err := saved.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	c.Version = "v3"
	r := New(opts)
	if err := r.Restore(c); err == nil {
		t.Fatal("want error for a checkpoint of another version")
	}
	if !r.peaks.IsEmpty() || r.samples != 0 {
		t.Error("refused checkpoint changed the recommender")
	}
}
