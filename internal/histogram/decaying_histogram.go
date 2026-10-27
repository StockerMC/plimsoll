/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package histogram

import (
	"fmt"
	"math"
	"time"
)

// Once the decay factor passes 2^maxDecayExponent the reference timestamp is
// shifted forward so the exponents stay small.
const maxDecayExponent = 100

// Each sample's weight is multiplied by 2^((sampleTime - referenceTimestamp) / halfLife),
// so a sample loses half its weight every halfLife. Only relative weights
// matter, so referenceTimestamp can move at any time; moving it is the same as
// multiplying every weight by a constant.
type decayingHistogram struct {
	histogram
	halfLife time.Duration
	// Always an integer multiple of halfLife.
	referenceTimestamp time.Time
}

// NewDecayingHistogram returns a Histogram whose samples lose half their
// weight every halfLife.
func NewDecayingHistogram(options HistogramOptions, halfLife time.Duration) Histogram {
	return &decayingHistogram{
		histogram:          *NewHistogram(options).(*histogram),
		halfLife:           halfLife,
		referenceTimestamp: time.Time{},
	}
}

func (h *decayingHistogram) Percentile(percentile float64) float64 {
	return h.histogram.Percentile(percentile)
}

func (h *decayingHistogram) AddSample(value float64, weight float64, time time.Time) {
	h.histogram.AddSample(value, weight*h.decayFactor(time), time)
}

func (h *decayingHistogram) SubtractSample(value float64, weight float64, time time.Time) {
	h.histogram.SubtractSample(value, weight*h.decayFactor(time), time)
}

func (h *decayingHistogram) Merge(other Histogram) {
	o := other.(*decayingHistogram)
	if h.halfLife != o.halfLife {
		panic("can't merge decaying histograms with different half life periods")
	}
	if h.referenceTimestamp.Before(o.referenceTimestamp) {
		h.shiftReferenceTimestamp(o.referenceTimestamp)
	} else if o.referenceTimestamp.Before(h.referenceTimestamp) {
		o.shiftReferenceTimestamp(h.referenceTimestamp)
	}
	h.histogram.Merge(&o.histogram)
}

func (h *decayingHistogram) Equals(other Histogram) bool {
	h2, typesMatch := other.(*decayingHistogram)
	return typesMatch && h.halfLife == h2.halfLife && h.referenceTimestamp.Equal(h2.referenceTimestamp) && h.histogram.Equals(&h2.histogram)
}

func (h *decayingHistogram) IsEmpty() bool {
	return h.histogram.IsEmpty()
}

func (h *decayingHistogram) String() string {
	return fmt.Sprintf("referenceTimestamp: %v, halfLife: %v\n%s", h.referenceTimestamp, h.halfLife, h.histogram.String())
}

func (h *decayingHistogram) shiftReferenceTimestamp(newReferenceTimestamp time.Time) {
	newReferenceTimestamp = newReferenceTimestamp.Round(h.halfLife)
	exponent := round(float64(h.referenceTimestamp.Sub(newReferenceTimestamp)) / float64(h.halfLife))
	h.scale(math.Ldexp(1., exponent))
	h.referenceTimestamp = newReferenceTimestamp
}

func (h *decayingHistogram) decayFactor(timestamp time.Time) float64 {
	maxAllowedTimestamp := h.referenceTimestamp.Add(
		time.Duration(int64(h.halfLife) * int64(maxDecayExponent)))
	if timestamp.After(maxAllowedTimestamp) {
		h.shiftReferenceTimestamp(timestamp)
	}
	return math.Exp2(float64(timestamp.Sub(h.referenceTimestamp)) / float64(h.halfLife))
}

func round(x float64) int {
	return int(math.Floor(x + 0.5))
}
