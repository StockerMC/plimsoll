package main

import (
	"slices"
	"time"
)

const tick = 15

type outcome struct {
	side, workload string
	replicas       int
	before, after  float64
	complete       bool
	target         float64
	ooms, oomsOff  int
	killsComplete  bool
	resizes        int
	restarts       int
	replacements   int
	readyLossS     int
	exposurePodMin float64
}

func parseTime(s string) (int64, error) {
	t, err := time.Parse(time.RFC3339, s)
	return t.Unix(), err
}

// summarize scores one workload. Before is the last fully Ready sample before the switch; after is
// the last sample, and only counts when every desired replica is Ready with no resize pending and
// applied resources equal to spec.
func summarize(r run, workload string) outcome {
	o := outcome{side: r.info["side"], workload: workload, target: r.targets[workload], killsComplete: true}
	deploys := map[int64]deploySample{}
	for _, d := range r.deployments {
		if d.workload == workload {
			deploys[d.time] = d
		}
	}
	byTime := map[int64][]podSample{}
	var times []int64
	for _, p := range r.pods {
		if p.workload != workload {
			continue
		}
		if _, ok := byTime[p.time]; !ok {
			times = append(times, p.time)
		}
		byTime[p.time] = append(byTime[p.time], p)
	}
	for t := range deploys {
		if _, ok := byTime[t]; !ok {
			times = append(times, t)
		}
	}
	slices.Sort(times)

	settled := func(t int64) (float64, bool) {
		d, ok := deploys[t]
		if !ok || d.desired == 0 || d.ready != d.desired {
			return 0, false
		}
		sum, live := 0.0, 0
		for _, p := range byTime[t] {
			if p.deleting {
				continue
			}
			live++
			if !p.ready || p.resizing || p.appliedRequest != p.specRequest || p.appliedLimit != p.specLimit {
				return 0, false
			}
			sum += p.specRequest
		}
		return sum, live == d.desired
	}

	firstSeen := map[string]int64{}
	baseRestarts := map[string]int{}
	lastRestarts := map[string]int{}
	lastApplied := map[string]float64{}
	var prev int64
	for _, t := range times {
		if prev != 0 && t-prev > 2*tick {
			o.killsComplete = false
		}
		prev = t
		on := t > r.switchTime
		if !on {
			if sum, ok := settled(t); ok {
				o.before = sum
				o.replicas = deploys[t].desired
			}
		} else if d := deploys[t]; d.ready < d.desired {
			o.readyLossS += tick
		}
		for _, p := range byTime[t] {
			if _, ok := firstSeen[p.uid]; !ok {
				firstSeen[p.uid] = t
				if on {
					o.replacements++
				}
			}
			if !on {
				baseRestarts[p.uid] = p.restarts
			}
			lastRestarts[p.uid] = max(lastRestarts[p.uid], p.restarts)
			if p.resizing || p.appliedRequest == 0 {
				continue
			}
			if old, ok := lastApplied[p.uid]; ok && on && old != p.appliedRequest {
				o.resizes++
			}
			lastApplied[p.uid] = p.appliedRequest
		}
		if on {
			o.exposurePodMin += float64(len(byTime[t])) * tick / 60
		}
	}
	if len(times) > 0 {
		o.after, o.complete = settled(times[len(times)-1])
	}
	for uid, n := range lastRestarts {
		o.restarts += n - baseRestarts[uid]
	}

	seen := map[[3]string]bool{}
	onTerms := 0
	for _, t := range r.terminations {
		key := [3]string{t.uid, t.container, t.containerID}
		if t.workload != workload || seen[key] {
			continue
		}
		seen[key] = true
		if t.finished <= r.switchTime {
			if t.reason == "OOMKilled" {
				o.oomsOff++
			}
			continue
		}
		onTerms++
		if t.reason == "OOMKilled" {
			o.ooms++
		}
	}
	// A restart with no matching termination record cannot be classified, so the kill count is a lower bound.
	if o.restarts > onTerms {
		o.killsComplete = false
	}
	return o
}
