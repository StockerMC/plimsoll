package trace

import (
	"cmp"
	"slices"
	"strconv"
)

const (
	updatePending = 9
	updateRunning = 10
)

type event struct {
	instance int
	time     int64
	typ      int
	reqMem   float64
}

// diff recovers each instance's starting request and the UPDATE_PENDING and
// UPDATE_RUNNING events that changed it. Any other event, including a
// restart under the same index, never counts as an update.
func diff(rows [][]string) (map[int]float64, []Update) {
	events := make([]event, 0, len(rows))
	for _, f := range rows {
		instance, _ := strconv.Atoi(f[1])
		t, _ := strconv.ParseInt(f[2], 10, 64)
		typ, _ := strconv.Atoi(f[3])
		req, _ := strconv.ParseFloat(f[4], 64)
		events = append(events, event{instance, t, typ, req})
	}
	// Events at one time are ordered by type, so a SCHEDULE at the trace
	// start carries the request the instance ran with.
	slices.SortFunc(events, func(a, b event) int {
		return cmp.Or(cmp.Compare(a.instance, b.instance), cmp.Compare(a.time, b.time), cmp.Compare(a.typ, b.typ))
	})
	requests := make(map[int]float64)
	last := make(map[int]float64)
	var updates []Update
	for _, e := range events {
		prev, seen := last[e.instance]
		if !seen || e.time == 0 {
			requests[e.instance] = e.reqMem
		} else if (e.typ == updatePending || e.typ == updateRunning) && e.reqMem != prev {
			updates = append(updates, Update{Time: Time(e.time), Instance: e.instance, ReqMem: e.reqMem})
		}
		last[e.instance] = e.reqMem
	}
	slices.SortStableFunc(updates, func(a, b Update) int { return a.Time.Compare(b.Time) })
	return requests, updates
}
