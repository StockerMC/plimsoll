package percentile

import (
	"math"
	"time"
)

// Defaults from upstream config/config.go and model/types.go.
const (
	targetPercentile   = 0.9
	lowerPercentile    = 0.5
	upperPercentile    = 0.95
	safetyMargin       = 0.15
	confidenceInterval = 24 * time.Hour
	maxResourceAmount  = 1e14
)

// scale clamps like upstream's ScaleResource. An empty history gives
// 0 * +Inf, which upstream leaves undefined; here it is 0.
func scale(amount, factor float64) float64 {
	v := amount * factor
	if v > maxResourceAmount {
		return maxResourceAmount
	}
	if v < 0 || math.IsNaN(v) {
		return 0
	}
	return v
}

func withMargin(base, fraction float64) float64 {
	return base + scale(base, fraction)
}

// confidence is the number of days of history, capped by the sample count so
// a sparse history does not count as a full day.
func confidence(first, last time.Time, samples int, samplesPerDay float64) float64 {
	lifespanDays := float64(last.Sub(first)) / float64(confidenceInterval)
	return math.Min(lifespanDays, float64(samples)/samplesPerDay)
}

func withConfidence(base, multiplier, exponent, confidence float64) float64 {
	return scale(base, math.Pow(1+multiplier/confidence, exponent))
}

func withMin(base, floor float64) float64 {
	return math.Max(base, floor)
}
