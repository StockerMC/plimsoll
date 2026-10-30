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
	offUsage       float64 // peak per-pod usage in updateMode Off; 0 when not sampled
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
// the sample at the scheduled end, and only counts when the desired replicas never changed from
// before and every one is Ready with no resize pending and applied resources equal to spec.
func summarize(r run, workload string) outcome {
	o := outcome{side: r.info["side"], workload: workload, target: r.targets[workload], killsComplete: r.watchLogged}
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

	type uidState struct {
		container              string
		baseRestarts, restarts int
		lastSeen, gone         int64
		terms, onTerms         int
	}
	uids := map[string]*uidState{}
	lastApplied := map[string]float64{}
	replicasChanged := false
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
		} else if d, ok := deploys[t]; !ok || d.desired != o.replicas {
			replicasChanged = true
		} else if d.ready < d.desired {
			o.readyLossS += tick
		}
		present := map[string]bool{}
		for _, p := range byTime[t] {
			present[p.uid] = true
			u, ok := uids[p.uid]
			if !ok {
				u = &uidState{container: p.container}
				uids[p.uid] = u
				if on {
					o.replacements++
				}
			}
			if !on {
				u.baseRestarts = p.restarts
			}
			u.restarts = max(u.restarts, p.restarts)
			u.lastSeen = t
			if p.resizing || p.appliedRequest == 0 {
				continue
			}
			if old, ok := lastApplied[p.uid]; ok && on && old != p.appliedRequest {
				o.resizes++
			}
			lastApplied[p.uid] = p.appliedRequest
		}
		for uid, u := range uids {
			if u.gone == 0 && !present[uid] {
				u.gone = t
			}
		}
		if on {
			o.exposurePodMin += float64(len(byTime[t])) * tick / 60
		}
	}
	if n := len(times); n > 0 && r.end > 0 && times[n-1] >= r.end && !replicasChanged {
		o.after, o.complete = settled(times[n-1])
	}

	seen := map[[3]string]bool{}
	for _, t := range r.terminations {
		key := [3]string{t.uid, t.container, t.containerID}
		if t.workload != workload || seen[key] {
			continue
		}
		seen[key] = true
		on := t.finished > r.switchTime
		if u := uids[t.uid]; u != nil && (u.container == "" || u.container == t.container) {
			u.terms++
			if on {
				u.onTerms++
			}
		}
		if !on {
			if t.reason == "OOMKilled" {
				o.oomsOff++
			}
			continue
		}
		if t.reason == "OOMKilled" {
			o.ooms++
		}
	}
	for _, u := range uids {
		o.restarts += u.restarts - u.baseRestarts
		// A restart with no termination record of its own cannot be classified.
		if u.restarts > u.terms || u.restarts-u.baseRestarts > u.onTerms {
			o.killsComplete = false
		}
		// A pod deleted while the watch was down may have taken a termination with it.
		for _, g := range r.watchGaps {
			if u.gone != 0 && g[0] <= u.gone && g[1] > u.lastSeen {
				o.killsComplete = false
			}
		}
	}

	pods := map[string]bool{}
	for _, p := range r.pods {
		if p.workload == workload {
			pods[p.pod] = true
		}
	}
	for _, s := range r.usage {
		if pods[s.pod] && s.time <= r.switchTime {
			o.offUsage = max(o.offUsage, s.memory)
		}
	}
	return o
}
