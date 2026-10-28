package recommender

import "github.com/StockerMC/plimsoll/internal/histogram"

// BucketTops returns the upper edge of every bucket. The last bucket has no
// upper edge, so its start stands in, as in upstream's Percentile.
func BucketTops(o histogram.HistogramOptions) []float64 {
	n := o.NumBuckets()
	tops := make([]float64, n)
	for k := 0; k < n-1; k++ {
		tops[k] = o.GetBucketStart(k + 1)
	}
	tops[n-1] = o.GetBucketStart(n - 1)
	return tops
}

// Counts bins the sample's peaks: entry k is the number of tasks whose peak
// fell in bucket k.
func (s Sample) Counts(o histogram.HistogramOptions) []float64 {
	counts := make([]float64, o.NumBuckets())
	for _, p := range s.Peaks {
		counts[o.FindBucket(p.Peak)]++
	}
	return counts
}
