package percentile

import (
	"fmt"
	"maps"
	"slices"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender"
)

// checkpointVersion changes whenever the buckets, the window rule or the
// confidence count change, so an old checkpoint is refused instead of misread.
const checkpointVersion = "plimsoll-v2"

var _ recommender.Checkpointer = (*Recommender)(nil)

// version also names the aggregation interval and half-life, since a
// histogram saved under one is misread under another.
func (r *Recommender) version() string {
	return fmt.Sprintf("%s aggregation=%s half-life=%s", checkpointVersion, r.opts.AggregationInterval, r.opts.HalfLife)
}

// Checkpoint saves the histogram as it is, open windows included, with the
// open windows themselves. Upstream's subtractCurrentContainerMemoryPeak
// drops open windows instead, but with a 24 h window that leaves a VPA
// younger than a day with an empty checkpoint.
func (r *Recommender) Checkpoint() (recommender.Checkpoint, error) {
	h, err := r.peaks.SaveToCheckpoint()
	if err != nil {
		return recommender.Checkpoint{}, err
	}
	c := recommender.Checkpoint{
		Version:   r.version(),
		Histogram: *h,
		First:     r.first,
		Last:      r.last,
		Samples:   r.samples,
	}
	for _, name := range slices.Sorted(maps.Keys(r.instances)) {
		s := r.instances[name]
		if s.windowEnd.After(r.last) {
			c.Windows = append(c.Windows, recommender.OpenWindow{Instance: name, End: s.windowEnd, Peak: s.peak, OOMPeak: s.oomPeak, LastSample: s.lastSample})
		}
	}
	return c, nil
}

// Restore loads a checkpoint into an empty recommender.
func (r *Recommender) Restore(c recommender.Checkpoint) error {
	if c.Version != r.version() {
		return fmt.Errorf("checkpoint version %q, want %q", c.Version, r.version())
	}
	peaks := histogram.NewDecayingHistogram(r.opts.Buckets, r.opts.HalfLife)
	if err := peaks.LoadFromCheckpoint(&c.Histogram); err != nil {
		return err
	}
	r.peaks = peaks
	r.first = c.First
	r.last = c.Last
	r.samples = c.Samples
	for _, w := range c.Windows {
		r.instances[w.Instance] = &instance{peak: w.Peak, oomPeak: w.OOMPeak, windowEnd: w.End, lastSample: w.LastSample}
	}
	return nil
}
