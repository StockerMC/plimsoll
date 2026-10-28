package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"

	"github.com/StockerMC/plimsoll/internal/metrics"
	"github.com/StockerMC/plimsoll/internal/trace"
)

// summary is one run's summary.json.
type summary struct {
	RunID          string `json:"run_id"`
	Commit         string `json:"commit"`
	Cell           string `json:"cell"`
	Recommender    string `json:"recommender"`
	Actuator       string `json:"actuator"`
	Split          string `json:"split"`
	Params         Params `json:"params"`
	Collections    int    `json:"collections"`
	Groups         int    `json:"groups"`
	CollectionDays int    `json:"collection_days"`
	WindowsScored  int    `json:"windows_scored"`
	// Instance-windows scored; exceedance windows over this is the
	// exceedance fraction.
	InstanceWindows int `json:"instance_windows"`
	SkippedRows     int `json:"skipped_rows"`
	// Mean and median over collections of the collection's mean over its
	// days.
	MeanRelativeSlack   maybe   `json:"mean_relative_slack"`
	MedianRelativeSlack float64 `json:"median_relative_slack"`
	// Sum over scored instance-windows of limit times seconds, in trace
	// memory units times seconds.
	MemoryTime                          float64 `json:"memory_time"`
	LimitChangesPerCollectionDay        float64 `json:"limit_changes_per_collection_day"`
	ExceedanceWindowsPerCollectionDay   float64 `json:"exceedance_windows_per_collection_day"`
	ExceedanceFreeCollectionDayFraction float64 `json:"exceedance_free_collection_day_fraction"`
	// Wall time of the pass that produced this run and the windows it
	// replayed per second over every run in the pass.
	PassSeconds      float64                  `json:"pass_seconds"`
	PassRuns         int                      `json:"pass_runs"`
	WindowsPerSecond float64                  `json:"windows_per_second"`
	Tracking         map[string]trackingBlock `json:"tracking"`
}

// trackingBlock aggregates the tracking metrics over one vertical_scaling
// group, as means and medians over collections.
type trackingBlock struct {
	Collections         int                `json:"collections"`
	Updates             int                `json:"updates"`
	MeanAbsRelError     map[string]maybe   `json:"mean_abs_rel_error"`
	MedianAbsRelError   map[string]float64 `json:"median_abs_rel_error"`
	PostUpdateMeanError map[string]maybe   `json:"post_update_mean_abs_rel_error"`
	// Collections with at least one scored post-update window.
	PostUpdateCollections int   `json:"post_update_collections"`
	DirectionAgreement    maybe `json:"direction_agreement"`
}

// maybe is a metric that is undefined over no collections; NaN is written
// as null, which encoding/json cannot represent otherwise.
type maybe float64

func (m maybe) MarshalJSON() ([]byte, error) {
	if math.IsNaN(float64(m)) {
		return []byte("null"), nil
	}
	return json.Marshal(float64(m))
}

func (m maybe) String() string {
	if math.IsNaN(float64(m)) {
		return "undefined"
	}
	return strconv.FormatFloat(float64(m), 'f', 3, 64)
}

func summarize(p *pass, i int, runID, commit, split string) summary {
	rs := p.results[i]
	r := p.runs[i]
	s := summary{RunID: runID, Commit: commit, Cell: "a", Recommender: r.Recommender, Actuator: r.Actuator,
		Split: split, Params: r.Params, Collections: len(rs), PassSeconds: p.seconds, PassRuns: len(p.runs),
		Tracking: make(map[string]trackingBlock)}
	groups := make(map[string]bool)
	var slacks []float64
	exceed, changes, free, windows := 0, 0, 0, 0
	for _, x := range rs {
		groups[x.Group] = true
		if x.Days == 0 {
			continue
		}
		slacks = append(slacks, x.MeanSlack)
		s.CollectionDays += x.Days
		s.WindowsScored += x.Summary.Windows
		s.InstanceWindows += x.InstanceWindows
		s.SkippedRows += x.Skipped
		s.MemoryTime += x.MemoryTime
		exceed += x.Exceedances
		changes += x.Changes
		free += x.FreeDays
		windows += x.Windows
	}
	s.Groups = len(groups)
	s.MeanRelativeSlack = maybe(mean(slacks))
	s.MedianRelativeSlack = metrics.Percentile(slacks, 0.5)
	if s.CollectionDays > 0 {
		s.LimitChangesPerCollectionDay = float64(changes) / float64(s.CollectionDays)
		s.ExceedanceWindowsPerCollectionDay = float64(exceed) / float64(s.CollectionDays)
		s.ExceedanceFreeCollectionDayFraction = float64(free) / float64(s.CollectionDays)
	}
	if p.seconds > 0 {
		s.WindowsPerSecond = float64(windows*len(p.runs)) / p.seconds
	}
	for name, vs := range map[string]int{"fully_automated": trace.FullyAutomated, "off": trace.Off} {
		s.Tracking[name] = aggregateTracking(rs, vs)
	}
	return s
}

func aggregateTracking(rs []result, verticalScaling int) trackingBlock {
	b := trackingBlock{MeanAbsRelError: map[string]maybe{}, MedianAbsRelError: map[string]float64{}, PostUpdateMeanError: map[string]maybe{}}
	errs := make(map[int][]float64)
	post := make(map[int][]float64)
	var agreement []float64
	for _, x := range rs {
		if x.VerticalScaling != verticalScaling || x.Days == 0 {
			continue
		}
		b.Collections++
		b.Updates += x.Tracking.Updates
		for _, lag := range lags {
			if e := x.Tracking.Error[lag]; !math.IsNaN(e) {
				errs[lag] = append(errs[lag], e)
			}
			if e := x.Tracking.PostUpdate[lag]; !math.IsNaN(e) {
				post[lag] = append(post[lag], e)
			}
		}
		if x.Tracking.Updates > 0 {
			agreement = append(agreement, x.Tracking.Agreement)
		}
	}
	for _, lag := range lags {
		k := strconv.Itoa(lag)
		b.MeanAbsRelError[k] = maybe(mean(errs[lag]))
		b.MedianAbsRelError[k] = metrics.Percentile(errs[lag], 0.5)
		b.PostUpdateMeanError[k] = maybe(mean(post[lag]))
	}
	b.PostUpdateCollections = len(post[0])
	b.DirectionAgreement = maybe(mean(agreement))
	return b
}

var collectionColumns = []string{"collection_id", "group", "vertical_scaling", "windows", "days", "mean_slack", "memory_time",
	"exceedances", "changes", "free_days", "updates", "err_lag0", "err_lag1", "err_lag2", "err_lag12",
	"post_lag0", "post_lag1", "post_lag2", "post_lag12", "agreement"}

func writeRun(dir string, s summary, rs []result) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), append(raw, '\n'), 0o644); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "collections.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Write(collectionColumns)
	for _, x := range rs {
		row := []string{strconv.FormatInt(x.ID, 10), x.Group, strconv.Itoa(x.VerticalScaling), strconv.Itoa(x.Windows),
			strconv.Itoa(x.Days), num(x.MeanSlack), num(x.MemoryTime), strconv.Itoa(x.Exceedances), strconv.Itoa(x.Changes),
			strconv.Itoa(x.FreeDays), strconv.Itoa(x.Tracking.Updates)}
		for _, lag := range lags {
			row = append(row, num(x.Tracking.Error[lag]))
		}
		for _, lag := range lags {
			row = append(row, num(x.Tracking.PostUpdate[lag]))
		}
		row = append(row, num(x.Tracking.Agreement))
		w.Write(row)
	}
	w.Flush()
	return w.Error()
}

func num(v float64) string {
	if math.IsNaN(v) {
		return ""
	}
	return strconv.FormatFloat(v, 'g', 10, 64)
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return math.NaN()
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func printSummary(s summary) {
	fmt.Printf("%s: %d collections, %d days, slack %.4f (median %.4f), exceedance/day %.4f (%.4f%% of %d instance-windows), changes/day %.4f, free days %.3f, memory-time %.6g\n",
		s.RunID, s.Collections, s.CollectionDays, s.MeanRelativeSlack, s.MedianRelativeSlack,
		s.ExceedanceWindowsPerCollectionDay, 100*s.ExceedanceWindowsPerCollectionDay*float64(s.CollectionDays)/float64(max(s.InstanceWindows, 1)), s.InstanceWindows,
		s.LimitChangesPerCollectionDay, s.ExceedanceFreeCollectionDayFraction, s.MemoryTime)
	if fa, ok := s.Tracking["fully_automated"]; ok && fa.Collections > 0 {
		fmt.Printf("  tracking fully_automated: %d collections, %d updates, error by lag %v, post-update %v, agreement %.3f\n",
			fa.Collections, fa.Updates, byLag(fa.MeanAbsRelError), byLag(fa.PostUpdateMeanError), fa.DirectionAgreement)
	}
}

func byLag(m map[string]maybe) []string {
	var out []string
	for _, lag := range lags {
		k := strconv.Itoa(lag)
		out = append(out, k+":"+strconv.FormatFloat(float64(m[k]), 'f', 4, 64))
	}
	return out
}
