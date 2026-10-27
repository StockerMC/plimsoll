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
	"testing"
	"time"
)

var startTime = time.Unix(1234567890, 0)

func TestPercentilesEmptyDecayingHistogram(t *testing.T) {
	h := NewDecayingHistogram(testHistogramOptions, time.Hour)
	for p := -0.5; p <= 1.5; p += 0.5 {
		if got := h.Percentile(p); got != 0.0 {
			t.Errorf("Percentile(%v) = %v, want 0", p, got)
		}
	}
}

// A heavy sample is almost, but not entirely, decayed after enough half lives.
func TestSimpleDecay(t *testing.T) {
	h := NewDecayingHistogram(testHistogramOptions, time.Hour)
	h.AddSample(2, 1000, startTime)
	// 20 half lives later the new sample weighs 2^20 * 0.001 > 1000 times more.
	h.AddSample(1, 1, startTime.Add(time.Hour*20))
	checkPercentile(t, h, 0.999, 2)
	checkPercentile(t, h, 1.0, 3)
}

// The histogram still works after the decay factor grows past 2^maxDecayExponent.
func TestLongtermDecay(t *testing.T) {
	h := NewDecayingHistogram(testHistogramOptions, time.Hour)
	h.AddSample(2, 1, startTime)
	h.AddSample(1, 1, startTime.Add(time.Hour*101))
	checkPercentile(t, h, 1.0, 2)
}

func TestDecayingHistogramPercentiles(t *testing.T) {
	h := NewDecayingHistogram(testHistogramOptions, time.Hour)
	timestamp := startTime
	// Values and weights 1, 2, 3, 4, each one half life after the previous.
	for i := 1; i <= 4; i++ {
		h.AddSample(float64(i), float64(i), timestamp)
		timestamp = timestamp.Add(time.Hour)
	}
	// bucket [1..2], weight 1 * 2^-3, percentiles ~  0% ...  2%
	// bucket [2..3], weight 2 * 2^-2, percentiles ~  3% ... 10%
	// bucket [3..4], weight 3 * 2^-1, percentiles ~ 11% ... 34%
	// bucket [4..5], weight 4 * 2^-0, percentiles ~ 35% ... 100%
	tests := []struct {
		percentile float64
		want       float64
	}{
		{0.00, 2},
		{0.02, 2},
		{0.03, 3},
		{0.10, 3},
		{0.11, 4},
		{0.34, 4},
		{0.35, 5},
		{1.00, 5},
	}
	for _, tt := range tests {
		checkPercentile(t, h, tt.percentile, tt.want)
	}
}

// With a fixed timestamp the decaying histogram matches a plain one.
func TestNoDecay(t *testing.T) {
	h := NewDecayingHistogram(testHistogramOptions, time.Hour)
	for i := 1; i <= 4; i++ {
		h.AddSample(float64(i), float64(i), startTime)
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

func TestDecayingHistogramMerge(t *testing.T) {
	h1 := NewDecayingHistogram(testHistogramOptions, time.Hour)
	h1.AddSample(1, 1, startTime)
	h1.AddSample(2, 1, startTime.Add(time.Hour))

	h2 := NewDecayingHistogram(testHistogramOptions, time.Hour)
	h2.AddSample(2, 1, startTime.Add(time.Hour*2))
	h2.AddSample(3, 1, startTime.Add(time.Hour))

	expected := NewDecayingHistogram(testHistogramOptions, time.Hour)
	expected.AddSample(2, 1, startTime.Add(time.Hour*2))
	expected.AddSample(2, 1, startTime.Add(time.Hour))
	expected.AddSample(3, 1, startTime.Add(time.Hour))
	expected.AddSample(1, 1, startTime)

	h1.Merge(h2)
	if !h1.Equals(expected) {
		t.Errorf("merged histogram\n%v\nwant\n%v", h1, expected)
	}
}

func TestReferenceTimestampShift(t *testing.T) {
	h := NewDecayingHistogram(testHistogramOptions, time.Hour).(*decayingHistogram)
	h.AddSample(2, 1, startTime)
	ref := startTime.Round(time.Hour)
	if !h.referenceTimestamp.Equal(ref) {
		t.Fatalf("referenceTimestamp = %v, want %v", h.referenceTimestamp, ref)
	}
	h.AddSample(2, 1, ref.Add(time.Hour*100))
	if !h.referenceTimestamp.Equal(ref) {
		t.Errorf("reference shifted at exactly 100 half lives: %v", h.referenceTimestamp)
	}
	h.AddSample(2, 1, ref.Add(time.Hour*101))
	want := ref.Add(time.Hour * 101)
	if !h.referenceTimestamp.Equal(want) {
		t.Errorf("referenceTimestamp = %v, want %v", h.referenceTimestamp, want)
	}
	checkPercentile(t, h, 1.0, 3)
}
