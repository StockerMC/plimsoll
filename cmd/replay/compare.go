package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/StockerMC/plimsoll/internal/metrics"
)

const (
	bootstrapResamples = 2000
	bootstrapSeed      = 1
)

// comparison is the paired difference of a candidate run against a baseline
// run over the collections both scored.
type comparison struct {
	Candidate   string `json:"candidate"`
	Baseline    string `json:"baseline"`
	Actuator    string `json:"actuator"`
	Split       string `json:"split"`
	Collections int    `json:"collections"`
	Groups      int    `json:"groups"`
	// Mean over collections of candidate slack minus baseline slack, with
	// its bootstrap 95% CI over groups.
	SlackDifference  float64 `json:"slack_difference"`
	SlackCILow       float64 `json:"slack_ci_low"`
	SlackCIHigh      float64 `json:"slack_ci_high"`
	CandidateSlack   float64 `json:"candidate_slack"`
	BaselineSlack    float64 `json:"baseline_slack"`
	MemoryTimeChange float64 `json:"memory_time_change"`
	// Candidate over baseline; null when the baseline has none.
	ExceedanceRatio  maybe   `json:"exceedance_ratio"`
	ChangesRatio     maybe   `json:"changes_ratio"`
	CandidateExceed  float64 `json:"candidate_exceedance_per_day"`
	BaselineExceed   float64 `json:"baseline_exceedance_per_day"`
	CandidateChanges float64 `json:"candidate_changes_per_day"`
	BaselineChanges  float64 `json:"baseline_changes_per_day"`
	// Collections where the candidate's slack is lower and its exceedance
	// per day is not higher.
	WinFraction float64 `json:"win_fraction"`
}

type collectionRow struct {
	group                               string
	windows, days, exceedances, changes int
	slack, memoryTime                   float64
}

func compareRuns(candidate, baseline, out string) error {
	var cs, bs summary
	if err := readJSON(filepath.Join(candidate, "summary.json"), &cs); err != nil {
		return err
	}
	if err := readJSON(filepath.Join(baseline, "summary.json"), &bs); err != nil {
		return err
	}
	if cs.Actuator != bs.Actuator {
		return fmt.Errorf("actuators differ: %s is %s, %s is %s", cs.RunID, cs.Actuator, bs.RunID, bs.Actuator)
	}
	if cs.Split != bs.Split {
		return fmt.Errorf("splits differ: %s is %s, %s is %s", cs.RunID, cs.Split, bs.RunID, bs.Split)
	}
	cand, err := readCollections(filepath.Join(candidate, "collections.csv"))
	if err != nil {
		return err
	}
	base, err := readCollections(filepath.Join(baseline, "collections.csv"))
	if err != nil {
		return err
	}
	c := comparison{Candidate: cs.RunID, Baseline: bs.RunID, Actuator: cs.Actuator, Split: cs.Split}
	groups := make(map[string][]float64)
	var diffs, cSlack, bSlack []float64
	var cMem, bMem float64
	var cExceed, bExceed, cChanges, bChanges, days, wins int
	if len(cand) != len(base) {
		return fmt.Errorf("%s has %d collections, %s has %d", cs.RunID, len(cand), bs.RunID, len(base))
	}
	for _, id := range slices.Sorted(maps.Keys(cand)) {
		x, y := cand[id], base[id]
		if _, ok := base[id]; !ok {
			return fmt.Errorf("collection %d is in %s but not %s", id, cs.RunID, bs.RunID)
		}
		if x.group != y.group || x.days != y.days || x.windows != y.windows {
			return fmt.Errorf("collection %d: group, days or windows differ between %s and %s", id, cs.RunID, bs.RunID)
		}
		if x.days == 0 {
			continue
		}
		c.Collections++
		d := x.slack - y.slack
		diffs = append(diffs, d)
		groups[x.group] = append(groups[x.group], d)
		cSlack = append(cSlack, x.slack)
		bSlack = append(bSlack, y.slack)
		cMem += x.memoryTime
		bMem += y.memoryTime
		cExceed += x.exceedances
		bExceed += y.exceedances
		cChanges += x.changes
		bChanges += y.changes
		days += x.days
		if x.slack < y.slack && float64(x.exceedances)/float64(x.days) <= float64(y.exceedances)/float64(y.days) {
			wins++
		}
	}
	if c.Collections == 0 {
		return fmt.Errorf("no collection scored by both runs")
	}
	c.Groups = len(groups)
	c.SlackDifference = mean(diffs)
	var byGroup [][]float64
	for _, g := range slices.Sorted(maps.Keys(groups)) {
		byGroup = append(byGroup, groups[g])
	}
	c.SlackCILow, c.SlackCIHigh = metrics.Bootstrap(byGroup, bootstrapResamples, bootstrapSeed)
	c.CandidateSlack, c.BaselineSlack = mean(cSlack), mean(bSlack)
	c.MemoryTimeChange = cMem/bMem - 1
	c.CandidateExceed, c.BaselineExceed = float64(cExceed)/float64(days), float64(bExceed)/float64(days)
	c.CandidateChanges, c.BaselineChanges = float64(cChanges)/float64(days), float64(bChanges)/float64(days)
	c.ExceedanceRatio = ratioOf(cExceed, bExceed)
	c.ChangesRatio = ratioOf(cChanges, bChanges)
	c.WinFraction = float64(wins) / float64(c.Collections)

	dir := filepath.Join(out, "compare-"+time.Now().Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := filepath.Join(dir, cs.RunID+"-vs-"+bs.RunID)
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(name+".json", append(raw, '\n'), 0o644); err != nil {
		return err
	}
	md := fmt.Sprintf("# %s vs %s\n\nActuator %s, split %s, %d collections in %d groups. Bootstrap 95%% CI over groups, %d resamples, seed %d.\n\n| metric | value |\n|---|---|\n| mean slack, candidate | %.4f |\n| mean slack, baseline | %.4f |\n| paired slack difference | %+.4f [%+.4f, %+.4f] |\n| memory-time change | %+.2f%% |\n| exceedance windows per collection-day, candidate / baseline | %.4f / %.4f (ratio %s) |\n| limit changes per collection-day, candidate / baseline | %.4f / %.4f (ratio %s) |\n| win fraction | %.3f |\n",
		cs.RunID, bs.RunID, c.Actuator, c.Split, c.Collections, c.Groups, bootstrapResamples, bootstrapSeed,
		c.CandidateSlack, c.BaselineSlack, c.SlackDifference, c.SlackCILow, c.SlackCIHigh, 100*c.MemoryTimeChange,
		c.CandidateExceed, c.BaselineExceed, c.ExceedanceRatio, c.CandidateChanges, c.BaselineChanges, c.ChangesRatio, c.WinFraction)
	if err := os.WriteFile(name+".md", []byte(md), 0o644); err != nil {
		return err
	}
	fmt.Print(md)
	return nil
}

func readCollections(path string) (map[int64]collectionRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	col := make(map[string]int)
	for i, name := range rows[0] {
		col[name] = i
	}
	for _, name := range []string{"collection_id", "group", "windows", "days", "exceedances", "changes", "mean_slack", "memory_time"} {
		if _, ok := col[name]; !ok {
			return nil, fmt.Errorf("%s: no %s column", path, name)
		}
	}
	out := make(map[int64]collectionRow, len(rows)-1)
	for i, r := range rows[1:] {
		id, errID := strconv.ParseInt(r[col["collection_id"]], 10, 64)
		windows, errWindows := strconv.Atoi(r[col["windows"]])
		days, errDays := strconv.Atoi(r[col["days"]])
		exceedances, errExceed := strconv.Atoi(r[col["exceedances"]])
		changes, errChanges := strconv.Atoi(r[col["changes"]])
		// A collection with no scored day has no slack and is skipped.
		slack, errSlack := math.NaN(), error(nil)
		if days != 0 || r[col["mean_slack"]] != "" {
			slack, errSlack = strconv.ParseFloat(r[col["mean_slack"]], 64)
		}
		memoryTime, errMem := strconv.ParseFloat(r[col["memory_time"]], 64)
		if err := errors.Join(errID, errWindows, errDays, errExceed, errChanges, errSlack, errMem); err != nil {
			return nil, fmt.Errorf("%s row %d: %w", path, i+2, err)
		}
		if _, dup := out[id]; dup {
			return nil, fmt.Errorf("%s: collection %d appears twice", path, id)
		}
		out[id] = collectionRow{group: r[col["group"]], windows: windows, days: days, exceedances: exceedances,
			changes: changes, slack: slack, memoryTime: memoryTime}
	}
	return out, nil
}

func ratioOf(a, b int) maybe {
	if b == 0 {
		return maybe(math.NaN())
	}
	return maybe(float64(a) / float64(b))
}
