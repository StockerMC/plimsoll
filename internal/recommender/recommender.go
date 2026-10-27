package recommender

import "time"

// InstancePeak is the peak memory of one task over a window.
type InstancePeak struct {
	Instance string
	Peak     float64
}

// Sample is one window: its start time and the peak of every task alive in
// it, keyed by pod name live and by instance index in replay.
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
// is called by the live binary when instance was OOMKilled at request; the
// replay never calls it because the trace records no kills.
type Recommender interface {
	Observe(Sample)
	RecordOOM(instance string, at time.Time, request float64) error
	Recommend() Recommendation
}
