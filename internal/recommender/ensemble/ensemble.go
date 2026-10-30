// Package ensemble configures the model selector with a grid of arg-min
// models and, by default, the moving-window percentile as one more arm, so
// the selector can fall back to the reference when the models cost more.
package ensemble

import (
	"time"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender/ml"
	"github.com/StockerMC/plimsoll/internal/recommender/window"
)

// Options configure the ensemble.
type Options struct {
	Buckets histogram.HistogramOptions
	// Floor on every recommendation.
	MinMemory float64
	// Limits at or above this get the 10% margin instead of 15%.
	LargeLimit float64
	// Floor on the bump an OOM adds to the request.
	OOMMinBump float64
	// Leave out the moving-window arm, the paper's configuration.
	ArgminOnly bool
	// The selector's global cost weights and decay; zero means the
	// defaults.
	Weights ml.Weights
	Decay   float64
	// Fall hysteresis on the selector's recommendation; zero windows
	// follows every fall at once.
	FallFraction float64
	FallWindows  int
	// With RatioWindows set, one arg-min arm per half-life whose margin is
	// the largest peak-to-raw ratio of its last RatioWindows windows, at
	// least RatioFloor, instead of the grid of fixed margins.
	RatioWindows int
	RatioFloor   float64
	// Divides every half-life, for workloads that replay a trace faster
	// than real time; zero is 1.
	TimeScale float64
	// Windows in the selector's max and cold start; zero is hourWindows.
	HourWindows int
}

var (
	halfLives = []time.Duration{6 * time.Hour, 12 * time.Hour, 24 * time.Hour, 48 * time.Hour, 7 * 24 * time.Hour}
	margins   = []float64{0.05, 0.10, 0.15, 0.25}
	// DefaultWeights and DefaultDecay are the selector's globals.
	DefaultWeights = ml.Weights{Overrun: 5, Underrun: 1, LimitChange: 0.1, ModelChange: 0.1}
)

const (
	DefaultDecay   = 0.2
	windowHalfLife = 48 * time.Hour
	percentile     = 0.98
	// An hour at the paper's 5-minute window; a shorter --memory-window
	// shortens the cold start and the max with it.
	hourWindows = 12
)

// New returns the configured selector.
func New(o Options) *ml.Selector {
	weights, costDecay := o.Weights, o.Decay
	if weights == (ml.Weights{}) {
		weights = DefaultWeights
	}
	if costDecay == 0 {
		costDecay = DefaultDecay
	}
	scale, hour := o.TimeScale, o.HourWindows
	if scale == 0 {
		scale = 1
	}
	if hour == 0 {
		hour = hourWindows
	}
	scaled := func(d time.Duration) time.Duration { return time.Duration(float64(d) / scale) }
	margin := window.StepMargin(o.LargeLimit)
	var arms []ml.Arm
	for _, hl := range halfLives {
		hl = scaled(hl)
		if o.RatioWindows > 0 {
			arms = append(arms, ml.NewRatio(o.Buckets, ml.NewArgmin(o.Buckets, hl, 0, weights), o.RatioWindows, o.RatioFloor))
			continue
		}
		for _, m := range margins {
			arms = append(arms, ml.NewArgmin(o.Buckets, hl, m, weights))
		}
	}
	if !o.ArgminOnly {
		// Only Limit() is read, so the arm needs no peak or hourly ring.
		arms = append(arms, window.New(window.Options{
			Buckets:    o.Buckets,
			HalfLife:   scaled(windowHalfLife),
			Stat:       window.Percentile,
			Percentile: percentile,
			Margin:     margin,
		}))
	}
	return ml.New(ml.Options{
		Buckets:      o.Buckets,
		Arms:         arms,
		Weights:      weights,
		Decay:        costDecay,
		MaxWindows:   hour,
		FallFraction: o.FallFraction,
		FallWindows:  o.FallWindows,
		ColdWindows:  hour,
		Margin:       margin,
		MinMemory:    o.MinMemory,
		OOMMinBump:   o.OOMMinBump,
	})
}
