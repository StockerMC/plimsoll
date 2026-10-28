// Package metrics scores a replayed limit series against the trace's usage:
// relative slack per collection-day (paper section 4.1.1, p95 of the window
// averages against the mean limit), provisioned memory-time, limit changes,
// limit-exceedance windows (instance-windows whose peak is above the limit,
// a simulated OOM proxy, never an OOM), tracking error against the request
// updates the trace recorded, and a paired bootstrap over groups.
package metrics
