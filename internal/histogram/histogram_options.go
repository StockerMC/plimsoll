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
	"errors"
	"fmt"
	"math"
)

// HistogramOptions define the number and size of buckets of a histogram.
type HistogramOptions interface {
	// NumBuckets returns the number of buckets in the histogram.
	NumBuckets() int
	// FindBucket returns the index of the bucket the value falls into. Values
	// outside the covered range map to the first or last bucket.
	FindBucket(value float64) int
	// GetBucketStart returns the start of the bucket with the given index,
	// which must be in [0, NumBuckets()-1].
	GetBucketStart(bucket int) float64
	// Epsilon returns the minimum weight for a bucket to count as non-empty.
	Epsilon() float64
}

// NewLinearHistogramOptions returns options for fixed-size buckets starting at
// 0.0, with the last bucket start at or above maxValue.
func NewLinearHistogramOptions(maxValue, bucketSize, epsilon float64) (HistogramOptions, error) {
	if maxValue <= 0.0 || bucketSize <= 0.0 || epsilon <= 0.0 {
		return nil, errors.New("maxValue, bucketSize and epsilon must be positive")
	}
	numBuckets := int(math.Ceil(maxValue/bucketSize)) + 1
	return &linearHistogramOptions{numBuckets, bucketSize, epsilon}, nil
}

// NewExponentialHistogramOptions returns options for buckets that grow by
// ratio. Bucket 0 covers [0, firstBucketSize), bucket n has size
// firstBucketSize * ratio^n and starts at firstBucketSize * (ratio^n - 1) / (ratio - 1).
// The last bucket start is at or above maxValue.
func NewExponentialHistogramOptions(maxValue, firstBucketSize, ratio, epsilon float64) (HistogramOptions, error) {
	if maxValue <= 0.0 || firstBucketSize <= 0.0 || ratio <= 1.0 || epsilon <= 0.0 {
		return nil, errors.New("maxValue, firstBucketSize and epsilon must be positive, ratio must be > 1.0")
	}
	numBuckets := int(math.Ceil(log(ratio, maxValue*(ratio-1)/firstBucketSize+1))) + 1
	return &exponentialHistogramOptions{numBuckets, firstBucketSize, ratio, epsilon}, nil
}

type linearHistogramOptions struct {
	numBuckets int
	bucketSize float64
	epsilon    float64
}

type exponentialHistogramOptions struct {
	numBuckets      int
	firstBucketSize float64
	ratio           float64
	epsilon         float64
}

func (o *linearHistogramOptions) NumBuckets() int {
	return o.numBuckets
}

func (o *linearHistogramOptions) FindBucket(value float64) int {
	bucket := int(value / o.bucketSize)
	if bucket < 0 {
		return 0
	}
	if bucket >= o.numBuckets {
		return o.numBuckets - 1
	}
	return bucket
}

func (o *linearHistogramOptions) GetBucketStart(bucket int) float64 {
	if bucket < 0 || bucket >= o.numBuckets {
		panic(fmt.Sprintf("index %d out of range [0..%d]", bucket, o.numBuckets-1))
	}
	return float64(bucket) * o.bucketSize
}

func (o *linearHistogramOptions) Epsilon() float64 {
	return o.epsilon
}

func (o *exponentialHistogramOptions) NumBuckets() int {
	return o.numBuckets
}

func (o *exponentialHistogramOptions) FindBucket(value float64) int {
	if value < o.firstBucketSize {
		return 0
	}
	bucket := int(log(o.ratio, value*(o.ratio-1)/o.firstBucketSize+1))
	if bucket >= o.numBuckets {
		return o.numBuckets - 1
	}
	return bucket
}

func (o *exponentialHistogramOptions) GetBucketStart(bucket int) float64 {
	if bucket < 0 || bucket >= o.numBuckets {
		panic(fmt.Sprintf("index %d out of range [0..%d]", bucket, o.numBuckets-1))
	}
	if bucket == 0 {
		return 0.0
	}
	return o.firstBucketSize * (math.Pow(o.ratio, float64(bucket)) - 1) / (o.ratio - 1)
}

func (o *exponentialHistogramOptions) Epsilon() float64 {
	return o.epsilon
}

func log(base, x float64) float64 {
	return math.Log(x) / math.Log(base)
}
