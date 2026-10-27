/*
Copyright 2017 The Kubernetes Authors.

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

package percentile

import (
	"math"
	"testing"
	"time"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender"
)

var (
	anyTime = time.Unix(0, 0)
	// Upstream's memory buckets from model/aggregations_config.go.
	memoryOptions, _ = histogram.NewExponentialHistogramOptions(1e12, 1e7, 1.05, 1e-4)
)

func inEpsilon(t *testing.T, got, want, maxRelativeError float64) {
	t.Helper()
	if math.Abs(got-want) > maxRelativeError*math.Abs(want) {
		t.Errorf("got %v, want %v within %v", got, want, maxRelativeError)
	}
}

func TestPercentileEstimator(t *testing.T) {
	r := New(Options{Buckets: memoryOptions, SamplesPerDay: 1440})
	r.Observe(recommender.Sample{Time: anyTime, Peaks: []recommender.InstancePeak{
		{Instance: "a", Peak: 1e9}, {Instance: "b", Peak: 2e9}, {Instance: "c", Peak: 3e9},
	}})
	// 5% relative error for histogram rounding.
	inEpsilon(t, r.peaks.Percentile(0.5), 2e9, 0.05)
}

func TestConfidenceMultiplier(t *testing.T) {
	r := New(Options{Buckets: memoryOptions, SamplesPerDay: 1440})
	noHistory := confidence(r.first, r.last, r.samples, r.opts.SamplesPerDay)
	if noHistory != 0 {
		t.Fatalf("confidence with no history = %v, want 0", noHistory)
	}
	if got := withConfidence(3.14e9, 1.0, 1.0, noHistory); got != maxResourceAmount {
		t.Errorf("no history, exponent 1: got %v, want %v", got, maxResourceAmount)
	}
	if got := withConfidence(3.14e9, 1.0, -1.0, noHistory); got != 0 {
		t.Errorf("no history, exponent -1: got %v, want 0", got)
	}

	ts := anyTime
	for i := 1; i <= 9; i++ {
		r.Observe(recommender.Sample{Time: ts, Peaks: []recommender.InstancePeak{{Instance: "a", Peak: 1e9}}})
		ts = ts.Add(2 * time.Minute)
	}
	// 9 samples against 1440 a day = 0.00625, below the 16 minute lifespan.
	c := confidence(r.first, r.last, r.samples, r.opts.SamplesPerDay)
	if c != 0.00625 {
		t.Fatalf("confidence = %v, want 0.00625", c)
	}
	// 3.14e9 * (1 + 0.1/0.00625)^2 = 9.0746e11.
	inEpsilon(t, withConfidence(3.14e9, 0.1, 2.0, c), 9.0746e11, 1e-9)
}

func TestMarginEstimator(t *testing.T) {
	inEpsilon(t, withMargin(3.14e9, 0.1), 3.14e9*1.1, 1e-12)
}

func TestMinResourcesEstimator(t *testing.T) {
	tests := []struct {
		base, min, want float64
	}{
		{4e8, 2e7, 4e8},
		{1e7, 2e7, 2e7},
	}
	for _, tt := range tests {
		if got := withMin(tt.base, tt.min); got != tt.want {
			t.Errorf("withMin(%v, %v) = %v, want %v", tt.base, tt.min, got, tt.want)
		}
	}
}
