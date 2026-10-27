package percentile

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender"
)

// Options are the inputs that differ between the live binary and the replay.
type Options struct {
	Buckets histogram.HistogramOptions
	// Samples that make one day of history: 1440 live, 288 in replay.
	SamplesPerDay float64
	// Floor on every recommendation, in the units of the peaks. Upstream uses
	// 250 MiB per pod; the replay uses 0.
	MinMemory float64
	// Window over which each task keeps one peak, and half-life of the
	// histogram's decay. Zero means upstream's 24 h.
	AggregationInterval time.Duration
	HalfLife            time.Duration
}

// Recommender is the upstream VPA memory recommender: a decaying histogram of
// per-task daily peaks read at fixed percentiles.
type Recommender struct {
	opts      Options
	peaks     histogram.Histogram
	instances map[string]*instance
	first     time.Time
	last      time.Time
	samples   int
}

var _ recommender.Recommender = (*Recommender)(nil)

// New returns an empty Recommender.
func New(opts Options) *Recommender {
	if opts.AggregationInterval == 0 {
		opts.AggregationInterval = defaultAggregationInterval
	}
	if opts.HalfLife == 0 {
		opts.HalfLife = defaultHalfLife
	}
	return &Recommender{
		opts:      opts,
		peaks:     histogram.NewDecayingHistogram(opts.Buckets, opts.HalfLife),
		instances: make(map[string]*instance),
	}
}

// Observe folds each task's window peak into that task's daily peak.
func (r *Recommender) Observe(s recommender.Sample) {
	if len(s.Peaks) == 0 {
		return
	}
	for _, p := range s.Peaks {
		r.instance(p.Instance).add(r.peaks, p.Peak, s.Time, false, r.opts.AggregationInterval)
	}
	r.samples += len(s.Peaks)
	if r.first.IsZero() || s.Time.Before(r.first) {
		r.first = s.Time
	}
	if s.Time.After(r.last) {
		r.last = s.Time
	}
}

// Recommend applies upstream's chain: percentile, safety margin, confidence
// multiplier on the bounds, then the floor.
func (r *Recommender) Recommend() recommender.Recommendation {
	c := confidence(r.first, r.last, r.samples, r.opts.SamplesPerDay)
	target := r.estimate(targetPercentile)
	lower := withConfidence(r.estimate(lowerPercentile), 0.001, -2, c)
	upper := withConfidence(r.estimate(upperPercentile), 1, 1, c)
	return recommender.Recommendation{
		Target: withMin(target, r.opts.MinMemory),
		Lower:  withMin(lower, r.opts.MinMemory),
		Upper:  withMin(upper, r.opts.MinMemory),
	}
}

// RecordOOM adds an artificial peak after an OOM kill of instance at request
// bytes: the larger of request plus 100 MiB and 1.2 times request, measured
// from the window's real peak if that is higher than the request.
func (r *Recommender) RecordOOM(instance string, at time.Time, request float64) error {
	s := r.instance(instance)
	if !s.lastSample.IsZero() && at.Before(s.lastSample.Add(-r.opts.AggregationInterval)) {
		return fmt.Errorf("oom at %v is older than the aggregation interval", at)
	}
	used := math.Max(request, s.peak)
	needed := math.Max(used+oomMinBumpUp, used*oomBumpUpRatio)
	if !s.add(r.peaks, needed, at, true, r.opts.AggregationInterval) {
		return errors.New("oom sample discarded")
	}
	return nil
}

func (r *Recommender) estimate(percentile float64) float64 {
	return withMargin(r.peaks.Percentile(percentile), safetyMargin)
}

func (r *Recommender) instance(name string) *instance {
	s, ok := r.instances[name]
	if !ok {
		s = &instance{}
		r.instances[name] = s
	}
	return s
}
