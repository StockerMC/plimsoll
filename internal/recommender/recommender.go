package recommender

import (
	"time"

	"github.com/StockerMC/plimsoll/internal/histogram"
)

// InstancePeak is the peak memory of one task over a window.
type InstancePeak struct {
	Instance string
	Peak     float64
}

// Sample is one window: its start time and the peak of every task alive in
// it, keyed by instance.
type Sample struct {
	Time  time.Time
	Peaks []InstancePeak
}

// Recommendation is a memory target with its lower and upper bound, in the
// units the peaks were given in.
type Recommendation struct {
	Target float64
	Lower  float64
	Upper  float64
}

// Recommender turns window samples into a memory recommendation. RecordOOM
// is called when instance was OOMKilled at request.
type Recommender interface {
	Observe(Sample)
	RecordOOM(instance string, at time.Time, request float64) error
	Recommend() Recommendation
}

// Checkpoint is the state a Checkpointer saves across restarts: the fields of
// upstream's VerticalPodAutoscalerCheckpoint status for memory.
type Checkpoint struct {
	Version   string
	Histogram histogram.Checkpoint
	First     time.Time
	Last      time.Time
	Samples   int
}

// Checkpointer is implemented by recommenders whose state can be saved and
// restored. Restore is called on an empty recommender before any Observe.
type Checkpointer interface {
	Checkpoint() (Checkpoint, error)
	Restore(Checkpoint) error
}
