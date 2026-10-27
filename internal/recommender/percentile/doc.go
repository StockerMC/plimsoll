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
// Options.SamplesPerDay: 1440 live, where every metrics-server scrape is a
// sample, and 288 in replay, where every five-minute window is one.
package percentile
