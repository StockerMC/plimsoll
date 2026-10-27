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

package histogram

import (
	"math"
	"testing"
	"time"
)

var (
	// Minimum precision of histogram values (relative).
	valueEpsilon = 1e-15
	// Minimum precision of histogram weights (absolute).
	weightEpsilon = 1e-15
	anyTime       = time.Time{}
	// Ten unit buckets, the same scheme upstream tests use.
	testHistogramOptions, _ = NewLinearHistogramOptions(10.0, 1.0, weightEpsilon)
)

func checkPercentile(t *testing.T, h Histogram, percentile, want float64) {
	t.Helper()
	got := h.Percentile(percentile)
	if math.Abs(got-want) > valueEpsilon*math.Abs(want) {
		t.Errorf("Percentile(%v) = %v, want %v", percentile, got, want)
	}
}

func TestPercentilesEmptyHistogram(t *testing.T) {
	h := NewHistogram(testHistogramOptions)
	for p := -0.5; p <= 1.5; p += 0.5 {
		if got := h.Percentile(p); got != 0.0 {
			t.Errorf("Percentile(%v) = %v, want 0", p, got)
		}
	}
}

// Histogram { 1: 1, 2: 2, 3: 3, 4: 4 }.
func TestPercentiles(t *testing.T) {
	h := NewHistogram(testHistogramOptions)
	for i := 1; i <= 4; i++ {
		h.AddSample(float64(i), float64(i), anyTime)
	}
	tests := []struct {
		percentile float64
		want       float64
	}{
		{0.0, 2},
		{0.1, 2},
		{0.2, 3},
		{0.3, 3},
		{0.4, 4},
		{0.5, 4},
		{0.6, 4},
		{0.7, 5},
		{0.8, 5},
		{0.9, 5},
		{1.0, 5},
	}
	for _, tt := range tests {
		checkPercentile(t, h, tt.percentile, tt.want)
	}
}

// Percentiles below 0 return the minimum value and above 1 the maximum.
func TestPercentileOutOfBounds(t *testing.T) {
	options, err := NewLinearHistogramOptions(1.0, 0.1, weightEpsilon)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHistogram(options)
	h.AddSample(0.1, 0.1, anyTime)
	h.AddSample(0.2, 0.2, anyTime)

	checkPercentile(t, h, -0.1, 0.2)
	checkPercentile(t, h, 1.1, 0.3)

	h.AddSample(0.0, 0.1, anyTime)
	h.AddSample(1.0, 0.2, anyTime)
	checkPercentile(t, h, -0.1, 0.1)
	checkPercentile(t, h, 1.1, 1.0)
}

func TestEmptyHistogram(t *testing.T) {
	options, err := NewLinearHistogramOptions(1.0, 0.1, weightEpsilon)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHistogram(options)
	if !h.IsEmpty() {
		t.Error("new histogram should be empty")
	}
	h.AddSample(0.1, weightEpsilon*2.5, anyTime)
	if h.IsEmpty() {
		t.Error("histogram with weight 2.5*epsilon should not be empty")
	}
	h.SubtractSample(0.1, weightEpsilon, anyTime)
	if h.IsEmpty() {
		t.Error("histogram with weight 1.5*epsilon should not be empty")
	}
	h.SubtractSample(0.1, weightEpsilon, anyTime)
	if !h.IsEmpty() {
		t.Error("histogram with weight 0.5*epsilon should be empty")
	}
}

func TestNonEmptyOnEpsilonAddition(t *testing.T) {
	options, err := NewLinearHistogramOptions(1.0, 0.1, weightEpsilon)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHistogram(options)
	if !h.IsEmpty() {
		t.Error("new histogram should be empty")
	}

	h.AddSample(9.9, weightEpsilon*3, anyTime)
	if h.IsEmpty() {
		t.Error("histogram should not be empty after a 3*epsilon sample")
	}
	h.AddSample(0.1, weightEpsilon*0.3, anyTime)
	if h.IsEmpty() {
		t.Error("histogram should stay non-empty after a 0.3*epsilon sample below the min bucket")
	}
	h.AddSample(999.9, weightEpsilon*0.3, anyTime)
	if h.IsEmpty() {
		t.Error("histogram should stay non-empty after a 0.3*epsilon sample above the max bucket")
	}
}

func TestHistogramMerge(t *testing.T) {
	h1 := NewHistogram(testHistogramOptions)
	h1.AddSample(1, 1, anyTime)
	h1.AddSample(2, 1, anyTime)

	h2 := NewHistogram(testHistogramOptions)
	h2.AddSample(2, 1, anyTime)
	h2.AddSample(3, 1, anyTime)

	expected := NewHistogram(testHistogramOptions)
	expected.AddSample(1, 1, anyTime)
	expected.AddSample(2, 1, anyTime)
	expected.AddSample(2, 1, anyTime)
	expected.AddSample(3, 1, anyTime)

	h1.Merge(h2)
	if !h1.Equals(expected) {
		t.Errorf("merged histogram\n%v\nwant\n%v", h1, expected)
	}
}
