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
	"fmt"
	"strings"
	"time"
)

// Histogram is an approximate distribution of some variable.
type Histogram interface {
	// Percentile returns an approximation of the given percentile, a number
	// between 0 and 1. An empty histogram returns 0.0.
	Percentile(percentile float64) float64

	// AddSample adds a sample with the given value and weight.
	AddSample(value float64, weight float64, time time.Time)

	// SubtractSample removes a sample with the given value and weight. The
	// total weight of samples with a given value cannot go negative.
	SubtractSample(value float64, weight float64, time time.Time)

	// Merge adds all samples from another histogram of the same type.
	Merge(other Histogram)

	// IsEmpty returns true if the histogram holds no weight.
	IsEmpty() bool

	// Equals compares with another histogram built on the same
	// HistogramOptions value. A different runtime type is never equal.
	Equals(other Histogram) bool

	// String returns a human-readable description of the histogram.
	String() string

	// SaveToCheckpoint returns a compact form of the histogram.
	SaveToCheckpoint() (*Checkpoint, error)

	// LoadFromCheckpoint adds the samples of a checkpoint to the histogram.
	LoadFromCheckpoint(*Checkpoint) error
}

// NewHistogram returns a new Histogram using the given options.
func NewHistogram(options HistogramOptions) Histogram {
	return &histogram{
		options:      options,
		bucketWeight: make([]float64, options.NumBuckets()),
		totalWeight:  0.0,
		minBucket:    options.NumBuckets() - 1,
		maxBucket:    0}
}

// Each bucket holds the total weight of the samples that fall into it.
// Percentile() returns the upper bound of the matching bucket with no
// interpolation. A bucket with weight below options.Epsilon() counts as empty.
type histogram struct {
	options      HistogramOptions
	bucketWeight []float64
	totalWeight  float64
	// First non-empty bucket, or the last bucket if none.
	minBucket int
	// Last non-empty bucket, or 0 if none.
	maxBucket int
}

func (h *histogram) AddSample(value float64, weight float64, time time.Time) {
	if weight < 0.0 {
		panic("sample weight must be non-negative")
	}
	bucket := h.options.FindBucket(value)
	h.bucketWeight[bucket] += weight
	h.totalWeight += weight
	if bucket < h.minBucket && h.bucketWeight[bucket] >= h.options.Epsilon() {
		h.minBucket = bucket
	}
	if bucket > h.maxBucket && h.bucketWeight[bucket] >= h.options.Epsilon() {
		h.maxBucket = bucket
	}
}

func safeSubtract(value, sub, epsilon float64) float64 {
	value -= sub
	if value < epsilon {
		return 0.0
	}
	return value
}

func (h *histogram) SubtractSample(value float64, weight float64, time time.Time) {
	if weight < 0.0 {
		panic("sample weight must be non-negative")
	}
	bucket := h.options.FindBucket(value)
	epsilon := h.options.Epsilon()

	h.totalWeight = safeSubtract(h.totalWeight, weight, epsilon)
	h.bucketWeight[bucket] = safeSubtract(h.bucketWeight[bucket], weight, epsilon)

	h.updateMinAndMaxBucket()
}

func (h *histogram) Merge(other Histogram) {
	o := other.(*histogram)
	if h.options != o.options {
		panic("can't merge histograms with different options")
	}
	for bucket := o.minBucket; bucket <= o.maxBucket; bucket++ {
		h.bucketWeight[bucket] += o.bucketWeight[bucket]
	}
	h.totalWeight += o.totalWeight
	if o.minBucket < h.minBucket {
		h.minBucket = o.minBucket
	}
	if o.maxBucket > h.maxBucket {
		h.maxBucket = o.maxBucket
	}
}

func (h *histogram) Percentile(percentile float64) float64 {
	if h.IsEmpty() {
		return 0.0
	}
	partialSum := 0.0
	threshold := percentile * h.totalWeight
	bucket := h.minBucket
	for ; bucket < h.maxBucket; bucket++ {
		partialSum += h.bucketWeight[bucket]
		if partialSum >= threshold {
			break
		}
	}
	if bucket < h.options.NumBuckets()-1 {
		return h.options.GetBucketStart(bucket + 1)
	}
	// The last bucket has no upper bound.
	return h.options.GetBucketStart(bucket)
}

func (h *histogram) IsEmpty() bool {
	return h.bucketWeight[h.minBucket] < h.options.Epsilon()
}

func (h *histogram) String() string {
	lines := []string{
		fmt.Sprintf("minBucket: %d, maxBucket: %d, totalWeight: %.3f",
			h.minBucket, h.maxBucket, h.totalWeight),
		"%-tile\tvalue",
	}
	for i := 0; i <= 100; i += 5 {
		lines = append(lines, fmt.Sprintf("%d\t%.3f", i, h.Percentile(0.01*float64(i))))
	}
	return strings.Join(lines, "\n")
}

func (h *histogram) Equals(other Histogram) bool {
	h2, typesMatch := other.(*histogram)
	if !typesMatch || h.options != h2.options || h.minBucket != h2.minBucket || h.maxBucket != h2.maxBucket {
		return false
	}
	for bucket := h.minBucket; bucket <= h.maxBucket; bucket++ {
		diff := h.bucketWeight[bucket] - h2.bucketWeight[bucket]
		if diff > 1e-15 || diff < -1e-15 {
			return false
		}
	}
	return true
}

func (h *histogram) updateMinAndMaxBucket() {
	epsilon := h.options.Epsilon()
	lastBucket := h.options.NumBuckets() - 1
	for h.bucketWeight[h.minBucket] < epsilon && h.minBucket < lastBucket {
		h.minBucket++
	}
	for h.bucketWeight[h.maxBucket] < epsilon && h.maxBucket > 0 {
		h.maxBucket--
	}
}

// scale multiplies all weights by a non-negative factor, which leaves the
// percentiles unchanged.
func (h *histogram) scale(factor float64) {
	if factor < 0.0 {
		panic("scale factor must be non-negative")
	}
	for bucket := h.minBucket; bucket <= h.maxBucket; bucket++ {
		h.bucketWeight[bucket] *= factor
	}
	h.totalWeight *= factor
	h.updateMinAndMaxBucket()
}
