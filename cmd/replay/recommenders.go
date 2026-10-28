package main

import (
	"fmt"
	"math"
	"time"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender"
	"github.com/StockerMC/plimsoll/internal/recommender/ensemble"
	"github.com/StockerMC/plimsoll/internal/recommender/ml"
	"github.com/StockerMC/plimsoll/internal/recommender/percentile"
	"github.com/StockerMC/plimsoll/internal/recommender/window"
	"github.com/StockerMC/plimsoll/internal/replay"
)

// Params are the tunable parameters of the percentile baseline and of the
// model selector's globals. Zero values mean the defaults.
type Params struct {
	Percentile struct {
		TargetPercentile float64 `json:"target_percentile"`
		HalfLifeHours    float64 `json:"half_life_hours"`
		Margin           float64 `json:"margin"`
	} `json:"percentile"`
	ML struct {
		Decay       float64 `json:"decay"`
		Overrun     float64 `json:"overrun"`
		Underrun    float64 `json:"underrun"`
		LimitChange float64 `json:"limit_change"`
		ModelChange float64 `json:"model_change"`
	} `json:"ml"`
}

// Trace memory is a fraction of the largest machine: the first
// bucket is 2^-17, the margin steps down at 0.05.
var buckets = func() histogram.HistogramOptions {
	o, err := histogram.NewExponentialHistogramOptions(1, math.Exp2(-17), 1.05, 1e-4)
	if err != nil {
		panic(err)
	}
	return o
}()

const (
	largeLimit    = 0.05
	samplesPerDay = 288
	hourWindows   = 12
)

var recommenders = []string{"percentile", "percentile-tuned", "window", "ml", "ensemble", "assigned", "naive"}

// newRecommender returns the constructor for name; nil for the trace's own
// assigned memory.
func newRecommender(name string, p Params) (func() recommender.Recommender, error) {
	switch name {
	case "percentile":
		return func() recommender.Recommender {
			return percentile.New(percentile.Options{Buckets: buckets, SamplesPerDay: samplesPerDay})
		}, nil
	case "percentile-tuned":
		return func() recommender.Recommender {
			return percentile.New(percentile.Options{
				Buckets:          buckets,
				SamplesPerDay:    samplesPerDay,
				HalfLife:         time.Duration(p.Percentile.HalfLifeHours * float64(time.Hour)),
				TargetPercentile: p.Percentile.TargetPercentile,
				SafetyMargin:     p.Percentile.Margin,
			})
		}, nil
	case "window":
		return func() recommender.Recommender {
			return window.New(window.Options{
				Buckets:     buckets,
				HalfLife:    48 * time.Hour,
				Stat:        window.Percentile,
				Percentile:  0.98,
				PeakWindows: hourWindows,
				Margin:      window.StepMargin(largeLimit),
				MaxWindows:  hourWindows,
			})
		}, nil
	case "ml", "ensemble":
		opts := ensemble.Options{
			Buckets:    buckets,
			LargeLimit: largeLimit,
			ArgminOnly: name == "ml",
			Weights:    ml.Weights{Overrun: p.ML.Overrun, Underrun: p.ML.Underrun, LimitChange: p.ML.LimitChange, ModelChange: p.ML.ModelChange},
			Decay:      p.ML.Decay,
		}
		return func() recommender.Recommender { return ensemble.New(opts) }, nil
	case "assigned":
		return nil, nil
	case "naive":
		return replay.NewNaive, nil
	}
	return nil, fmt.Errorf("unknown recommender %q", name)
}

func newActuator(name string) (replay.NewActuator, error) {
	switch name {
	case "every":
		return replay.Every, nil
	case "updater":
		return replay.Updater, nil
	}
	return nil, fmt.Errorf("unknown actuator %q", name)
}
