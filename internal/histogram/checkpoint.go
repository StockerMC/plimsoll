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
	"time"
)

// MaxCheckpointWeight is the weight the largest bucket is scaled to when a
// histogram is saved.
const MaxCheckpointWeight uint32 = 10000

// Checkpoint is the saved form of a histogram, the fields of upstream's
// HistogramCheckpoint.
type Checkpoint struct {
	ReferenceTimestamp time.Time
	BucketWeights      map[int]uint32
	TotalWeight        float64
}

// SaveToCheckpoint is upstream's SaveToChekpoint. Weights are rescaled so the
// largest bucket is MaxCheckpointWeight; buckets that round to zero are dropped.
func (h *histogram) SaveToCheckpoint() (*Checkpoint, error) {
	result := Checkpoint{
		BucketWeights: make(map[int]uint32),
		TotalWeight:   h.totalWeight,
	}
	maxWeight := 0.
	for bucket := h.minBucket; bucket <= h.maxBucket; bucket++ {
		if h.bucketWeight[bucket] > maxWeight {
			maxWeight = h.bucketWeight[bucket]
		}
	}
	ratio := float64(MaxCheckpointWeight) / maxWeight
	for bucket := h.minBucket; bucket <= h.maxBucket; bucket++ {
		newWeight := uint32(round(h.bucketWeight[bucket] * ratio))
		if newWeight > 0 {
			result.BucketWeights[bucket] = newWeight
		}
	}
	return &result, nil
}

// LoadFromCheckpoint adds the checkpoint's samples to the histogram.
func (h *histogram) LoadFromCheckpoint(checkpoint *Checkpoint) error {
	if checkpoint == nil {
		return errors.New("cannot load from empty checkpoint")
	}
	if checkpoint.TotalWeight < 0.0 {
		return fmt.Errorf("cannot load checkpoint with negative weight %v", checkpoint.TotalWeight)
	}
	sum := int64(0)
	for bucket, weight := range checkpoint.BucketWeights {
		sum += int64(weight)
		if bucket >= h.options.NumBuckets() {
			return fmt.Errorf("checkpoint has bucket %v that is exceeding histogram buckets %v", bucket, h.options.NumBuckets())
		}
		if bucket < 0 {
			return fmt.Errorf("checkpoint has a negative bucket %v", bucket)
		}
	}
	if sum == 0 {
		return nil
	}
	ratio := checkpoint.TotalWeight / float64(sum)
	for bucket, weight := range checkpoint.BucketWeights {
		if bucket < h.minBucket {
			h.minBucket = bucket
		}
		if bucket > h.maxBucket {
			h.maxBucket = bucket
		}
		h.bucketWeight[bucket] += float64(weight) * ratio
	}
	h.totalWeight += checkpoint.TotalWeight
	// Rounding can leave the smallest bucket under epsilon after a load, which
	// would make the histogram read as empty (kubernetes/autoscaler#7726).
	h.updateMinAndMaxBucket()
	return nil
}

func (h *decayingHistogram) SaveToCheckpoint() (*Checkpoint, error) {
	checkpoint, err := h.histogram.SaveToCheckpoint()
	if err != nil {
		return checkpoint, err
	}
	checkpoint.ReferenceTimestamp = h.referenceTimestamp
	return checkpoint, nil
}

func (h *decayingHistogram) LoadFromCheckpoint(checkpoint *Checkpoint) error {
	if err := h.histogram.LoadFromCheckpoint(checkpoint); err != nil {
		return err
	}
	h.referenceTimestamp = checkpoint.ReferenceTimestamp
	return nil
}
