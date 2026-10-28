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

// The last of eight daily peaks is in a window that is still open, so a
// restored recommender holds only the first seven: its target matches a
// recommender that saw seven days, not eight (p90 lands on 7e8, not 8e8).
func TestCheckpointRoundTrip(t *testing.T) {
	opts := Options{Buckets: memoryOptions, SamplesPerDay: 288}
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
	seven := New(opts)
	daily(seven, 7)
	inEpsilon(t, restored.Recommend().Target, seven.Recommend().Target, 1e-9)
	if restored.Recommend().Target == saved.Recommend().Target {
		t.Error("restored target includes the open window's peak")
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
	c.Version = "v3"
	r := New(opts)
	if err := r.Restore(c); err == nil {
		t.Fatal("want error for a checkpoint of another version")
	}
	if !r.peaks.IsEmpty() || r.samples != 0 {
		t.Error("refused checkpoint changed the recommender")
	}
}
