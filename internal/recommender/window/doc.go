// Package window implements the moving-window memory recommenders from the
// Autopilot paper (Rzadca et al., EuroSys '20), section 3.2.2: the peak over
// the last N windows, the decayed average and the percentile of
// load-adjusted usage, each with a safety margin and the max over the last
// hour of recommendations.
package window
