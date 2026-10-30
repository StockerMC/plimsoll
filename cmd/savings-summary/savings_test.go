package main

import (
	"cmp"
	"testing"
)

const mi = 1 << 20

func pod(t int64, uid string, spec, applied float64, restarts int) podSample {
	return podSample{time: t, workload: "w", uid: uid, ready: true, specRequest: spec * mi, specLimit: spec * mi,
		appliedRequest: applied * mi, appliedLimit: applied * mi, resizing: spec != applied, restarts: restarts}
}

func deploy(t int64, desired, ready int) deploySample {
	return deploySample{time: t, workload: "w", desired: desired, ready: ready}
}

func term(uid, id, reason string) termination {
	return termination{workload: "w", uid: uid, container: "memload", containerID: id, reason: reason, finished: 110}
}

func TestSummarize(t *testing.T) {
	warm := []podSample{pod(90, "a", 1024, 1024, 0), pod(90, "b", 1024, 1024, 0)}
	deleted := pod(120, "a", 1024, 1024, 0)
	deleted.deleting = true
	tests := []struct {
		name         string
		pods         []podSample
		ready        int
		terms        []termination
		complete     bool
		after        float64
		ooms         int
		resizes      int
		replacements int
		desired      int   // at the last sample; 0 is 2
		end          int64 // scheduled end; 0 is 120
		gaps         [][2]int64
		lowerBound   bool // the kill count is incomplete
	}{
		{
			name:     "completed in-place resize",
			pods:     []podSample{pod(105, "a", 512, 1024, 0), pod(105, "b", 1024, 1024, 0), pod(120, "a", 512, 512, 1), pod(120, "b", 1024, 1024, 0)},
			ready:    2,
			terms:    []termination{term("a", "1", "Error")},
			complete: true, after: 1536, resizes: 1,
		},
		{
			name:  "spec-only pending resize is not reclaimed",
			pods:  []podSample{pod(105, "a", 512, 1024, 0), pod(105, "b", 1024, 1024, 0), pod(120, "a", 512, 1024, 0), pod(120, "b", 1024, 1024, 0)},
			ready: 2,
		},
		{
			name:     "replacement pod at the new request",
			pods:     []podSample{pod(105, "a", 1024, 1024, 0), pod(105, "b", 1024, 1024, 0), deleted, pod(120, "b", 1024, 1024, 0), pod(120, "c", 512, 512, 0)},
			ready:    2,
			terms:    []termination{term("a", "1", "Error")},
			complete: true, after: 1536, replacements: 1,
		},
		{
			name:  "absent replica is not reclaimed",
			pods:  []podSample{pod(105, "a", 1024, 1024, 0), pod(105, "b", 1024, 1024, 0), pod(120, "b", 1024, 1024, 0)},
			ready: 1,
		},
		{
			name:     "duplicate termination counts once",
			pods:     []podSample{pod(105, "a", 1024, 1024, 0), pod(105, "b", 1024, 1024, 0), pod(120, "a", 1024, 1024, 1), pod(120, "b", 1024, 1024, 0)},
			ready:    2,
			terms:    []termination{term("a", "1", "OOMKilled"), term("a", "1", "OOMKilled")},
			complete: true, after: 2048, ooms: 1,
		},
		{
			name:     "OOM kill on a pod deleted before the next sample",
			pods:     []podSample{pod(105, "a", 1024, 1024, 0), pod(105, "b", 1024, 1024, 0), pod(120, "b", 1024, 1024, 0), pod(120, "c", 1024, 1024, 0)},
			ready:    2,
			terms:    []termination{term("a", "1", "OOMKilled")},
			complete: true, after: 2048, ooms: 1, replacements: 1,
		},
		{
			name:  "run cut short of the scheduled end",
			pods:  []podSample{pod(105, "a", 512, 512, 1), pod(105, "b", 512, 512, 1), pod(120, "a", 512, 512, 1), pod(120, "b", 512, 512, 1)},
			ready: 2,
			terms: []termination{term("a", "1", "Error"), term("b", "2", "Error")},
			end:   135, resizes: 2,
		},
		{
			name:    "scaling down is not reclaimed",
			pods:    []podSample{pod(105, "a", 1024, 1024, 0), pod(105, "b", 1024, 1024, 0), pod(120, "b", 1024, 1024, 0)},
			ready:   1,
			desired: 1,
		},
		{
			name:     "restart matched only by another pod's termination",
			pods:     []podSample{pod(105, "a", 1024, 1024, 0), pod(105, "b", 1024, 1024, 0), pod(120, "a", 1024, 1024, 1), pod(120, "c", 1024, 1024, 0)},
			ready:    2,
			terms:    []termination{term("b", "2", "Error")},
			complete: true, after: 2048, replacements: 1, lowerBound: true,
		},
		{
			name:     "pod deleted while the termination watch was down",
			pods:     []podSample{pod(105, "a", 1024, 1024, 0), pod(105, "b", 1024, 1024, 0), pod(120, "b", 1024, 1024, 0), pod(120, "c", 1024, 1024, 0)},
			ready:    2,
			gaps:     [][2]int64{{108, 125}},
			complete: true, after: 2048, replacements: 1, lowerBound: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desired, end := cmp.Or(tt.desired, 2), cmp.Or(tt.end, 120)
			r := run{
				info:         map[string]string{"side": "s"},
				switchTime:   100,
				end:          end,
				pods:         append(append([]podSample{}, warm...), tt.pods...),
				deployments:  []deploySample{deploy(90, 2, 2), deploy(105, 2, 2), deploy(120, desired, tt.ready)},
				terminations: tt.terms,
				watchLogged:  true,
				watchGaps:    tt.gaps,
			}
			o := summarize(r, "w")
			if o.before != 2048*mi {
				t.Errorf("before = %v MiB, want 2048", o.before/mi)
			}
			if o.complete != tt.complete || tt.complete && o.after != tt.after*mi {
				t.Errorf("after = %v MiB complete %v, want %v complete %v", o.after/mi, o.complete, tt.after, tt.complete)
			}
			if o.ooms != tt.ooms || o.killsComplete == tt.lowerBound {
				t.Errorf("ooms = %d complete %v, want %d complete %v", o.ooms, o.killsComplete, tt.ooms, !tt.lowerBound)
			}
			if o.resizes != tt.resizes || o.replacements != tt.replacements {
				t.Errorf("resizes, replacements = %d, %d, want %d, %d", o.resizes, o.replacements, tt.resizes, tt.replacements)
			}
		})
	}
}
