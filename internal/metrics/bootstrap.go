package metrics

import (
	"math/rand/v2"
	"slices"
)

// Bootstrap returns the 2.5th and 97.5th percentiles of the mean of the
// values over resamples drawn by group with replacement, so values that
// share a group are never split between the sample and its complement.
func Bootstrap(groups [][]float64, resamples int, seed uint64) (lo, hi float64) {
	r := rand.New(rand.NewPCG(seed, 0))
	means := make([]float64, 0, resamples)
	for range resamples {
		sum, n := 0.0, 0
		for range groups {
			for _, v := range groups[r.IntN(len(groups))] {
				sum += v
				n++
			}
		}
		means = append(means, sum/float64(n))
	}
	slices.Sort(means)
	return means[int(0.025*float64(len(means)-1))], means[int(0.975*float64(len(means)-1))]
}
