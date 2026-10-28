package metrics

import (
	"math"
	"testing"
)

func points(limits []float64) []Point {
	ps := make([]Point, len(limits))
	for i, l := range limits {
		ps[i] = Point{Day: i / 3, Limit: l}
	}
	return ps
}

// Two instances over three windows of 300 s at limit 10, one day. The six
// window averages are 2, 4, 6, 8, 5, 7, whose nearest-rank p95 is 8, so the
// slack is (10 - 8) / 10. The peaks 11 and 12 are above the limit, 10 is
// not: two exceedance windows, no exceedance-free day, and memory-time
// 6 x 10 x 300.
func TestSummarizeScoresOneDay(t *testing.T) {
	s := Series{Points: []Point{
		{Limit: 10, Usage: []Usage{{0, 2, 3, 300, false}, {1, 4, 11, 300, true}}},
		{Limit: 10, Usage: []Usage{{0, 6, 9, 300, false}, {1, 8, 10, 300, false}}},
		{Limit: 10, Usage: []Usage{{0, 5, 12, 300, true}, {1, 7, 5, 300, false}}},
	}}
	got := Summarize(s)
	want := Summary{Days: 1, Windows: 3, InstanceWindows: 6, MeanSlack: 0.2, MemoryTime: 18000, Exceedances: 2}
	if math.Abs(got.MeanSlack-want.MeanSlack) > 1e-12 {
		t.Errorf("slack %v, want %v", got.MeanSlack, want.MeanSlack)
	}
	got.MeanSlack = want.MeanSlack
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// Limits 10, 10, 12, 12, 9 change twice. Scoring from the fourth window
// still counts the last change, and the one before it belongs to the cold
// start.
func TestSummarizeCountsChangesFromScoredWindows(t *testing.T) {
	for _, tt := range []struct{ from, want int }{{0, 2}, {3, 1}} {
		if got := Summarize(Series{Points: points([]float64{10, 10, 12, 12, 9}), From: tt.from}).Changes; got != tt.want {
			t.Errorf("from %d: %d changes, want %d", tt.from, got, tt.want)
		}
	}
}

// The request steps 10 to 20 at window 2 and back at window 6; the limit
// follows the first step two windows late and never the second. Lag 0
// errors 0, 0, .5, .5, 0, 0, 1, 1 average 3/8; at lag 1 the limit is held
// against the previous window's request, errors 0, 0, .5, 0, 0, 0, 1 over
// seven windows. The post-update windows (two after each update) are 3, 4
// and 7 with errors .5, 0, 1 at both lags. The first direction is followed
// at window 4, the second never.
func TestTrackLagsAndPostUpdateWindows(t *testing.T) {
	req := []float64{10, 10, 20, 20, 20, 20, 10, 10}
	lim := []float64{10, 10, 10, 10, 20, 20, 20, 20}
	var s Series
	for i := range req {
		s.Points = append(s.Points, Point{Limit: lim[i], Request: req[i], Updated: i == 2 || i == 6})
	}
	got := Track(s, []int{0, 1}, 2)
	want := Tracking{
		Error:      map[int]float64{0: 3.0 / 8, 1: 1.5 / 7},
		PostUpdate: map[int]float64{0: 0.5, 1: 0.5},
		Agreement:  0.5,
		Updates:    2,
	}
	for lag := range want.Error {
		if math.Abs(got.Error[lag]-want.Error[lag]) > 1e-12 || math.Abs(got.PostUpdate[lag]-want.PostUpdate[lag]) > 1e-12 {
			t.Errorf("lag %d: error %v post-update %v, want %v %v", lag, got.Error[lag], got.PostUpdate[lag], want.Error[lag], want.PostUpdate[lag])
		}
	}
	if got.Agreement != want.Agreement || got.Updates != want.Updates {
		t.Errorf("agreement %v over %d updates, want %v over %d", got.Agreement, got.Updates, want.Agreement, want.Updates)
	}
}

// Two groups with means 0 and 1 give resample means 0, 0.5 and 1 with
// chances 1/4, 1/2 and 1/4, so the 2.5th and 97.5th percentiles are 0 and 1.
// Swapped, collapsed or out-of-range indices fail.
func TestBootstrapIntervalOfTwoGroups(t *testing.T) {
	lo, hi := Bootstrap([][]float64{{0}, {1}}, 2000, 1)
	if lo != 0 || hi != 1 {
		t.Errorf("CI [%v, %v], want [0, 1]", lo, hi)
	}
}
