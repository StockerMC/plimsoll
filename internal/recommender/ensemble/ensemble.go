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
}

var (
	halfLives = []time.Duration{6 * time.Hour, 12 * time.Hour, 24 * time.Hour, 48 * time.Hour, 7 * 24 * time.Hour}
	margins   = []float64{0, 0.05, 0.10, 0.15, 0.25}
	weights   = ml.Weights{Overrun: 5, Underrun: 1, LimitChange: 0.1, ModelChange: 0.1}
)

const (
	windowHalfLife = 48 * time.Hour
	percentile     = 0.98
	// An hour at the paper's 5-minute window; a shorter --memory-window
	// shortens the cold start and the max with it.
	hourWindows = 12
	costDecay   = 0.2
)

// New returns the configured selector.
func New(o Options) *ml.Selector {
	margin := window.StepMargin(o.LargeLimit)
	var arms []ml.Arm
	for _, hl := range halfLives {
		for _, m := range margins {
			arms = append(arms, ml.NewArgmin(o.Buckets, hl, m, weights))
		}
	}
	if !o.ArgminOnly {
		// Only Limit() is read, so the arm needs no peak or hourly ring.
		arms = append(arms, window.New(window.Options{
			Buckets:    o.Buckets,
			HalfLife:   windowHalfLife,
			Stat:       window.Percentile,
			Percentile: percentile,
			Margin:     margin,
		}))
	}
	return ml.New(ml.Options{
		Buckets:     o.Buckets,
		Arms:        arms,
		Weights:     weights,
		Decay:       costDecay,
		MaxWindows:  hourWindows,
		ColdWindows: hourWindows,
		Margin:      margin,
		MinMemory:   o.MinMemory,
		OOMMinBump:  o.OOMMinBump,
	})
}
