package histogram

import "testing"

// Bucket counts for the options upstream builds in model/aggregations_config.go.
func TestUpstreamBucketCount(t *testing.T) {
	const (
		growth  = 0.05
		epsilon = 0.001 * 0.1
	)
	tests := []struct {
		name            string
		maxValue        float64
		firstBucketSize float64
		want            int
	}{
		{"memory bytes", 1e12, 1e7, 176},
		{"cpu cores", 1000.0, 0.01, 176},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o, err := NewExponentialHistogramOptions(tt.maxValue, tt.firstBucketSize, 1+growth, epsilon)
			if err != nil {
				t.Fatal(err)
			}
			if got := o.NumBuckets(); got != tt.want {
				t.Errorf("NumBuckets() = %d, want %d", got, tt.want)
			}
			last := o.GetBucketStart(o.NumBuckets() - 1)
			if last < tt.maxValue {
				t.Errorf("last bucket starts at %v, below max %v", last, tt.maxValue)
			}
		})
	}
}
