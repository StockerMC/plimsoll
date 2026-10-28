package percentile

import (
	"fmt"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender"
)

// CheckpointVersion changes whenever the buckets, the window rule or the
// confidence count change, so an old checkpoint is refused instead of misread.
const CheckpointVersion = "plimsoll-v1"

var _ recommender.Checkpointer = (*Recommender)(nil)

// Checkpoint saves the histogram as it is, open windows included. Upstream's
// subtractCurrentContainerMemoryPeak drops them so a task that opens a new
// window after the restart is not counted twice, but with a 24 h window that
// leaves a VPA younger than a day with an empty checkpoint. Here every
// restart adds the current window's peak again, since Restore opens fresh
// windows, so a crash-looping recommender over-weights today's peak.
func (r *Recommender) Checkpoint() (recommender.Checkpoint, error) {
	h, err := r.peaks.SaveToCheckpoint()
	if err != nil {
		return recommender.Checkpoint{}, err
	}
	return recommender.Checkpoint{
		Version:   CheckpointVersion,
		Histogram: *h,
		First:     r.first,
		Last:      r.last,
		Samples:   r.samples,
	}, nil
}

// Restore loads a checkpoint into an empty recommender. Task windows are not
// saved, so each task opens a fresh one at its next sample.
func (r *Recommender) Restore(c recommender.Checkpoint) error {
	if c.Version != CheckpointVersion {
		return fmt.Errorf("checkpoint version %q, want %q", c.Version, CheckpointVersion)
	}
	peaks := histogram.NewDecayingHistogram(r.opts.Buckets, r.opts.HalfLife)
	if err := peaks.LoadFromCheckpoint(&c.Histogram); err != nil {
		return err
	}
	r.peaks = peaks
	r.first = c.First
	r.last = c.Last
	r.samples = c.Samples
	return nil
}
