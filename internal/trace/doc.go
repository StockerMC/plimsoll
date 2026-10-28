// Package trace reads the memory-only Borg 2019 extract (gzip CSV, sorted by
// collection) one collection at a time, builds its five-minute windows of
// per-instance memory peaks, and recovers the memory request updates recorded
// in its instance events.
package trace
