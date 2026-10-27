// Package percentile is a memory-only port of the upstream VPA recommender at
// tag vertical-pod-autoscaler-1.8.1: the estimator chain in
// pkg/recommender/logic/estimator.go and logic/recommender.go, the per-task
// memory window and OOM bump in model/container.go, and the defaults in
// pkg/recommender/config/config.go.
//
// Upstream counts samples for the confidence multiplier only when it adds a
// CPU sample, once per container per scrape, and normalizes the count to one
// sample a minute. A memory-only port that counted its own daily peaks would
// put confidence near zero and the upper bound near infinity, so this package
// counts one sample per instance in every Observe and divides by
// Options.SamplesPerDay, the number of windows in a day: 288 for the
// five-minute windows the live binary and the replay both use by default.
package percentile
