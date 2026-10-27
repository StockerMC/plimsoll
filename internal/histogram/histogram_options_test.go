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

import "testing"

const epsilon = 0.001

func TestLinearHistogramOptions(t *testing.T) {
	o, err := NewLinearHistogramOptions(5.0, 0.3, epsilon)
	if err != nil {
		t.Fatal(err)
	}
	if o.Epsilon() != epsilon {
		t.Errorf("Epsilon() = %v, want %v", o.Epsilon(), epsilon)
	}
	if o.NumBuckets() != 18 {
		t.Errorf("NumBuckets() = %d, want 18", o.NumBuckets())
	}

	starts := []struct {
		bucket int
		want   float64
	}{
		{0, 0.0},
		{17, 5.1},
	}
	for _, tt := range starts {
		if got := o.GetBucketStart(tt.bucket); got != tt.want {
			t.Errorf("GetBucketStart(%d) = %v, want %v", tt.bucket, got, tt.want)
		}
	}

	buckets := []struct {
		value float64
		want  int
	}{
		{-1.0, 0},
		{0.0, 0},
		{1.3, 4},
		{100.0, 17},
	}
	for _, tt := range buckets {
		if got := o.FindBucket(tt.value); got != tt.want {
			t.Errorf("FindBucket(%v) = %d, want %d", tt.value, got, tt.want)
		}
	}
}

func TestExponentialHistogramOptions(t *testing.T) {
	o, err := NewExponentialHistogramOptions(500.0, 40.0, 1.5, epsilon)
	if err != nil {
		t.Fatal(err)
	}
	if o.Epsilon() != epsilon {
		t.Errorf("Epsilon() = %v, want %v", o.Epsilon(), epsilon)
	}
	if o.NumBuckets() != 6 {
		t.Errorf("NumBuckets() = %d, want 6", o.NumBuckets())
	}

	starts := []struct {
		bucket int
		want   float64
	}{
		{0, 0.0},
		{1, 40.0},
		{2, 100.0},
		{3, 190.0},
		{4, 325.0},
		{5, 527.5},
	}
	for _, tt := range starts {
		if got := o.GetBucketStart(tt.bucket); got != tt.want {
			t.Errorf("GetBucketStart(%d) = %v, want %v", tt.bucket, got, tt.want)
		}
	}

	buckets := []struct {
		value float64
		want  int
	}{
		{-1.0, 0},
		{39.99, 0},
		{40.0, 1},
		{100.0, 2},
		{900.0, 5},
	}
	for _, tt := range buckets {
		if got := o.FindBucket(tt.value); got != tt.want {
			t.Errorf("FindBucket(%v) = %d, want %d", tt.value, got, tt.want)
		}
	}
}
