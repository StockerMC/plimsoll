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
	"testing"
	"time"
)

func TestHistogramSaveToCheckpoint(t *testing.T) {
	h := NewHistogram(testHistogramOptions)
	h.AddSample(1, 1, anyTime)
	s, err := h.SaveToCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	bucket := testHistogramOptions.FindBucket(1)
	if s.TotalWeight != 1 {
		t.Errorf("TotalWeight = %v, want 1", s.TotalWeight)
	}
	if len(s.BucketWeights) != 1 || s.BucketWeights[bucket] != MaxCheckpointWeight {
		t.Errorf("BucketWeights = %v, want {%d: %d}", s.BucketWeights, bucket, MaxCheckpointWeight)
	}
}

func TestHistogramSaveToCheckpointDropsRelativelySmallValues(t *testing.T) {
	h := NewHistogram(testHistogramOptions)
	h.AddSample(1, 1, anyTime)
	h.AddSample(2, 100000, anyTime)
	s, err := h.SaveToCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalWeight != 100001 {
		t.Errorf("TotalWeight = %v, want 100001", s.TotalWeight)
	}
	bucket := testHistogramOptions.FindBucket(2)
	if len(s.BucketWeights) != 1 || s.BucketWeights[bucket] != MaxCheckpointWeight {
		t.Errorf("BucketWeights = %v, want {%d: %d}", s.BucketWeights, bucket, MaxCheckpointWeight)
	}
}

func TestHistogramLoadFromCheckpoint(t *testing.T) {
	h := NewHistogram(testHistogramOptions).(*histogram)
	err := h.LoadFromCheckpoint(&Checkpoint{TotalWeight: 6, BucketWeights: map[int]uint32{0: 1, 1: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if h.totalWeight != 6 || h.bucketWeight[0] != 2 || h.bucketWeight[1] != 4 {
		t.Errorf("total %v, buckets %v %v; want 6, 2 4", h.totalWeight, h.bucketWeight[0], h.bucketWeight[1])
	}
}

// kubernetes/autoscaler#7726: a bucket just above epsilon next to one near
// MaxCheckpointWeight*epsilon drops under epsilon on load, and the histogram
// read as empty.
func TestHistogramIsNotEmptyAfterSavingAndLoadingCheckpointsWithBoundaryValues(t *testing.T) {
	h := NewHistogram(testHistogramOptions)
	h.AddSample(1, weightEpsilon, anyTime)
	h.AddSample(2, float64(MaxCheckpointWeight)*weightEpsilon-weightEpsilon, anyTime)
	s, err := h.SaveToCheckpoint()
	if err != nil {
		t.Fatal(err)
	}
	loaded := NewHistogram(testHistogramOptions)
	if err := loaded.LoadFromCheckpoint(s); err != nil {
		t.Fatal(err)
	}
	if loaded.IsEmpty() {
		t.Error("histogram is empty after a save and load")
	}
}

func TestDecayingHistogramLoadFromCheckpoint(t *testing.T) {
	timestamp := time.Date(2018, time.January, 2, 3, 4, 5, 0, time.UTC)
	d := NewDecayingHistogram(testHistogramOptions, time.Hour).(*decayingHistogram)
	err := d.LoadFromCheckpoint(&Checkpoint{
		TotalWeight:        6,
		BucketWeights:      map[int]uint32{0: 1},
		ReferenceTimestamp: timestamp,
	})
	if err != nil {
		t.Fatal(err)
	}
	if d.IsEmpty() {
		t.Error("histogram is empty after load")
	}
	if !d.referenceTimestamp.Equal(timestamp) {
		t.Errorf("referenceTimestamp = %v, want %v", d.referenceTimestamp, timestamp)
	}
}
