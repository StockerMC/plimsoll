package percentile

import (
	"fmt"
	"math"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender"
)

// CheckpointVersion changes whenever the buckets, the window rule or the
// confidence count change, so an old checkpoint is refused instead of misread.
const CheckpointVersion = "plimsoll-v1"

var _ recommender.Checkpointer = (*Recommender)(nil)

// Checkpoint saves the histogram without the peaks of windows that are still
// open, like upstream's subtractCurrentContainerMemoryPeak: after a restart
// those tasks open new windows and would otherwise be counted twice.
func (r *Recommender) Checkpoint() (recommender.Checkpoint, error) {
	peaks := histogram.NewDecayingHistogram(r.opts.Buckets, r.opts.HalfLife)
	peaks.Merge(r.peaks)
	for _, s := range r.instances {
		if r.last.Before(s.windowEnd) {
			peaks.SubtractSample(math.Max(s.peak, s.oomPeak), 1.0, s.windowEnd)
		}
	}
	h, err := peaks.SaveToCheckpoint()
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
