package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

var sides = []string{"plimsoll", "ensemble", "held", "upstream"}

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: savings-summary RAWDIR OUTDIR")
	}
	raw, out := os.Args[1], os.Args[2]
	paths, err := filepath.Glob(filepath.Join(raw, "*", "run.txt"))
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatal(err)
	}
	var runs []run
	var outcomes []outcome
	for _, p := range paths {
		r, err := readRun(filepath.Dir(p))
		if err != nil {
			log.Fatal(err)
		}
		runs = append(runs, r)
		var names []string
		for _, d := range r.deployments {
			if !slices.Contains(names, d.workload) {
				names = append(names, d.workload)
			}
		}
		slices.Sort(names)
		for _, w := range names {
			outcomes = append(outcomes, summarize(r, w))
		}
	}
	slices.SortStableFunc(outcomes, func(a, b outcome) int { return slices.Index(sides, a.side) - slices.Index(sides, b.side) })

	keys := []string{"side", "namespace", "batch", "mode", "recommender_flags", "binary_vcs.revision", "binary_vcs.modified", "binary_sha256", "config", "config_sha256", "updater_args", "upstream_recommender_pod", "upstream_recommender_pod_end", "off_s", "on_s", "begin", "switch", "end"}
	write(filepath.Join(out, "runs.csv"), keys, func(row func([]string)) {
		for _, r := range runs {
			var cells []string
			for _, k := range keys {
				cells = append(cells, r.info[k])
			}
			row(cells)
		}
	})
	write(filepath.Join(out, "timeline.csv"), []string{"side", "workload", "since_switch_s", "desired", "ready", "requested_mib", "applied_mib"}, func(row func([]string)) {
		for _, r := range runs {
			for _, line := range timeline(r) {
				row(line)
			}
		}
	})
	write(filepath.Join(out, "workloads.csv"), workloadHeader, func(row func([]string)) {
		for _, o := range outcomes {
			row(workloadRow(o))
		}
	})
	f, err := os.Create(filepath.Join(out, "summary.md"))
	if err != nil {
		log.Fatal(err)
	}
	writeMarkdown(io.MultiWriter(f, os.Stdout), outcomes)
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}

func write(path string, header []string, rows func(func([]string))) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	w := csv.NewWriter(f)
	w.Write(header)
	rows(func(cells []string) { w.Write(cells) })
	w.Flush()
	if err := w.Error(); err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
}

func timeline(r run) [][]string {
	type key struct {
		t int64
		w string
	}
	req, applied := map[key]float64{}, map[key]float64{}
	for _, p := range r.pods {
		if !p.deleting {
			req[key{p.time, p.workload}] += p.specRequest
			applied[key{p.time, p.workload}] += p.appliedRequest
		}
	}
	var lines [][]string
	for _, d := range r.deployments {
		k := key{d.time, d.workload}
		lines = append(lines, []string{r.info["side"], d.workload, strconv.FormatInt(d.time-r.switchTime, 10),
			strconv.Itoa(d.desired), strconv.Itoa(d.ready), mib(req[k]), mib(applied[k])})
	}
	return lines
}

var workloadHeader = []string{"side", "workload", "replicas", "requested_mib_before", "requested_mib_after", "reclaimed_pct", "target_mib", "off_usage_mib", "provisioned_x_before", "provisioned_x_after", "oom_kills", "completed_resizes", "container_restarts", "pod_replacements", "sampled_readiness_loss_s", "exposure_pod_min"}

func workloadRow(o outcome) []string {
	after, pct := "incomplete", "incomplete"
	if o.complete && o.before > 0 {
		after, pct = mib(o.after), percent(o.before, o.after)
	}
	usage, xBefore, xAfter := "", "", ""
	if o.offUsage > 0 && o.replicas > 0 {
		perPod := o.offUsage * float64(o.replicas)
		usage, xBefore = mib(o.offUsage), ratio(o.before/perPod)
		if o.complete {
			xAfter = ratio(o.after / perPod)
		}
	}
	return []string{o.side, o.workload, strconv.Itoa(o.replicas), mib(o.before), after, pct, mib(o.target), usage, xBefore, xAfter,
		kills(o.ooms, o.killsComplete), strconv.Itoa(o.resizes), strconv.Itoa(o.restarts), strconv.Itoa(o.replacements),
		strconv.Itoa(o.readyLossS), strconv.FormatFloat(o.exposurePodMin, 'f', 0, 64)}
}

type total struct {
	side                                   string
	before, after                          float64
	complete, killsComplete                bool
	ooms, oomsOff, resizes, restarts, repl int
	readyLossS                             int
	exposure                               float64
	workloads                              int
}

func totals(outcomes []outcome) []total {
	var ts []total
	for _, side := range sides {
		t := total{side: side, complete: true, killsComplete: true}
		for _, o := range outcomes {
			if o.side != side {
				continue
			}
			t.workloads++
			t.before += o.before
			t.after += o.after
			t.complete = t.complete && o.complete && o.before > 0
			t.killsComplete = t.killsComplete && o.killsComplete
			t.ooms += o.ooms
			t.oomsOff += o.oomsOff
			t.resizes += o.resizes
			t.restarts += o.restarts
			t.repl += o.replacements
			t.readyLossS += o.readyLossS
			t.exposure += o.exposurePodMin
		}
		if t.workloads > 0 {
			ts = append(ts, t)
		}
	}
	return ts
}

func writeMarkdown(w io.Writer, outcomes []outcome) {
	row := func(cells ...string) { fmt.Fprintln(w, "| "+strings.Join(cells, " | ")+" |") }
	fmt.Fprintln(w, "Requested memory on synthetic kind workloads with memory request = limit; it says nothing about physical memory, nodes or cost. Before: the last fully Ready sample in updateMode Off. After: the sample at the scheduled end, counted only if the desired replicas never changed, every one is Ready and applied resources equal spec with no resize pending. OOM kills: distinct OOMKilled terminations after the switch; a trailing + marks a lower bound (sampling gap, termination watch down while a pod was deleted, or a restart with no termination record on its own pod). Readiness loss counts 15 s samples with fewer Ready replicas than desired, so a restart shorter than the sampling interval can go unseen. Usage is the peak per-pod working set from metrics-server during updateMode Off; provisioned x is requested memory over that usage.")
	fmt.Fprintln(w)
	ts := totals(outcomes)
	var upstream *total
	for i := range ts {
		if ts[i].side == "upstream" {
			upstream = &ts[i]
		}
	}
	header := []string{"recommender", "workloads", "requested MiB before", "after", "reclaimed %", "reclaimed MiB vs upstream", "OOM kills", "OOM kills in Off", "completed resizes", "container restarts", "pod replacements", "sampled readiness loss s", "exposure pod-min"}
	row(header...)
	row(slices.Repeat([]string{"---"}, len(header))...)
	for _, t := range ts {
		after, pct, vs := "incomplete", "incomplete", ""
		if t.complete {
			after, pct = mib(t.after), percent(t.before, t.after)
			if upstream != nil && upstream.complete {
				vs = mib((t.before - t.after) - (upstream.before - upstream.after))
			}
		}
		row(t.side, strconv.Itoa(t.workloads), mib(t.before), after, pct, vs, kills(t.ooms, t.killsComplete), strconv.Itoa(t.oomsOff),
			strconv.Itoa(t.resizes), strconv.Itoa(t.restarts), strconv.Itoa(t.repl), strconv.Itoa(t.readyLossS), strconv.FormatFloat(t.exposure, 'f', 0, 64))
	}
	fmt.Fprintln(w)
	row(workloadHeader...)
	row(slices.Repeat([]string{"---"}, len(workloadHeader))...)
	for _, o := range outcomes {
		row(workloadRow(o)...)
	}
}

func kills(n int, complete bool) string {
	if complete {
		return strconv.Itoa(n)
	}
	return strconv.Itoa(n) + "+"
}

func ratio(x float64) string {
	return strconv.FormatFloat(x, 'f', 2, 64)
}

func percent(before, after float64) string {
	return strconv.FormatFloat(100*(before-after)/before, 'f', 1, 64)
}

func mib(bytes float64) string {
	// Rounding a tiny negative difference would print "-0".
	return strconv.FormatFloat(math.Round(bytes/(1<<20))+0, 'f', 0, 64)
}
