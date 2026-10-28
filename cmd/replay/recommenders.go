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
// model selector's globals, the latter once for ml and ensemble, once for
// ensemble-held and once for ensemble-ratio. Zero values mean the defaults.
type Params struct {
	Percentile struct {
		TargetPercentile float64 `json:"target_percentile"`
		HalfLifeHours    float64 `json:"half_life_hours"`
		Margin           float64 `json:"margin"`
	} `json:"percentile"`
	ML    selectorParams `json:"ml"`
	Held  selectorParams `json:"ensemble-held"`
	Ratio selectorParams `json:"ensemble-ratio"`
}

type selectorParams struct {
	Decay        float64 `json:"decay"`
	Overrun      float64 `json:"overrun"`
	Underrun     float64 `json:"underrun"`
	LimitChange  float64 `json:"limit_change"`
	ModelChange  float64 `json:"model_change"`
	FallFraction float64 `json:"fall_fraction"`
	FallWindows  int     `json:"fall_windows"`
	RatioWindows int     `json:"ratio_windows"`
	RatioFloor   float64 `json:"ratio_floor"`
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

var recommenders = []string{"percentile", "percentile-tuned", "window", "ml", "ensemble", "ensemble-held", "ensemble-ratio", "assigned", "naive"}

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
		if p.ML.FallFraction != 0 || p.ML.FallWindows != 0 {
			return nil, fmt.Errorf("%s has no fall hysteresis; use ensemble-held", name)
		}
		if p.ML.RatioWindows != 0 || p.ML.RatioFloor != 0 {
			return nil, fmt.Errorf("%s has no ratio margin; use ensemble-ratio", name)
		}
		return newEnsemble(name == "ml", p.ML), nil
	case "ensemble-held":
		if p.Held.RatioWindows != 0 || p.Held.RatioFloor != 0 {
			return nil, fmt.Errorf("%s has no ratio margin; use ensemble-ratio", name)
		}
		return newEnsemble(false, p.Held), nil
	case "ensemble-ratio":
		if p.Ratio.RatioWindows == 0 {
			return nil, fmt.Errorf("%s needs ratio_windows", name)
		}
		return newEnsemble(false, p.Ratio), nil
	case "assigned":
		return nil, nil
	case "naive":
		return replay.NewNaive, nil
	}
	return nil, fmt.Errorf("unknown recommender %q", name)
}

func newEnsemble(argminOnly bool, p selectorParams) func() recommender.Recommender {
	opts := ensemble.Options{
		Buckets:      buckets,
		LargeLimit:   largeLimit,
		ArgminOnly:   argminOnly,
		Weights:      ml.Weights{Overrun: p.Overrun, Underrun: p.Underrun, LimitChange: p.LimitChange, ModelChange: p.ModelChange},
		Decay:        p.Decay,
		FallFraction: p.FallFraction,
		FallWindows:  p.FallWindows,
		RatioWindows: p.RatioWindows,
		RatioFloor:   p.RatioFloor,
	}
	return func() recommender.Recommender { return ensemble.New(opts) }
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
