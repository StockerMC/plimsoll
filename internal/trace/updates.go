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

// diff recovers every change of each instance's request, from its first
// event on, and the subset made by UPDATE_PENDING and UPDATE_RUNNING events.
// A restart under the same index with an unchanged request is no change.
func diff(rows [][]string) (requests, updates []Update) {
	events := make([]event, 0, len(rows))
	for _, f := range rows {
		req, err := strconv.ParseFloat(f[4], 64)
		if err != nil {
			continue
		}
		instance, _ := strconv.Atoi(f[1])
		t, _ := strconv.ParseInt(f[2], 10, 64)
		typ, _ := strconv.Atoi(f[3])
		events = append(events, event{instance, t, typ, req})
	}
	// Events at one time are ordered by type, so a SCHEDULE at the trace
	// start carries the request the instance ran with.
	slices.SortFunc(events, func(a, b event) int {
		return cmp.Or(cmp.Compare(a.instance, b.instance), cmp.Compare(a.time, b.time), cmp.Compare(a.typ, b.typ))
	})
	last := make(map[int]float64)
	for _, e := range events {
		prev, seen := last[e.instance]
		last[e.instance] = e.reqMem
		if seen && e.reqMem == prev {
			continue
		}
		u := Update{Time: Time(e.time), Instance: e.instance, ReqMem: e.reqMem}
		requests = append(requests, u)
		if seen && e.time != 0 && (e.typ == updatePending || e.typ == updateRunning) {
			updates = append(updates, u)
		}
	}
	byTime := func(a, b Update) int { return a.Time.Compare(b.Time) }
	slices.SortStableFunc(requests, byTime)
	slices.SortStableFunc(updates, byTime)
	return requests, updates
}
