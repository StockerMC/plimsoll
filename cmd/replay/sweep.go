package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"

	"github.com/StockerMC/plimsoll/internal/replay"
)

// Exceedance and limit changes per day may be at most this much above the
// frozen window reference for a sweep row to be eligible.
const sweepTolerance = 1.10

type gridEntry struct {
	Name        string `json:"name"`
	Recommender string `json:"recommender"`
	Params      Params `json:"params"`
}

func sweepRuns(path string) ([]run, error) {
	var grid []gridEntry
	if err := readJSON(path, &grid); err != nil {
		return nil, err
	}
	var runs []run
	for _, g := range grid {
		newRec, err := newRecommender(g.Recommender, g.Params)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run{Name: g.Name, Recommender: g.Recommender, Actuator: "every", Params: g.Params, newRec: newRec, newAct: replay.Every})
	}
	return runs, nil
}

// writeSweep writes one row per run and marks, per recommender, the
// eligible row with the lowest mean slack. The reference is the run named
// window; without it no row is eligible.
func writeSweep(path string, p *pass) error {
	rows := make([]summary, len(p.runs))
	ref := -1
	commit := gitCommit()
	for i := range p.runs {
		rows[i] = summarize(p, i, p.runs[i].Name, commit, p.split)
		if p.runs[i].Name == "window" {
			ref = i
		}
	}
	eligible := make([]bool, len(rows))
	chosen := make(map[string]int)
	for i, s := range rows {
		if ref < 0 || s.Recommender == "window" {
			continue
		}
		r := rows[ref]
		eligible[i] = s.ExceedanceWindowsPerCollectionDay <= sweepTolerance*r.ExceedanceWindowsPerCollectionDay &&
			s.LimitChangesPerCollectionDay <= sweepTolerance*r.LimitChangesPerCollectionDay
		if !eligible[i] {
			continue
		}
		if j, ok := chosen[s.Recommender]; !ok || s.MeanRelativeSlack < rows[j].MeanRelativeSlack {
			chosen[s.Recommender] = i
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Write([]string{"run", "recommender", "params", "commit", "collections", "collection_days", "mean_slack",
		"exceedance_per_day", "changes_per_day", "free_day_fraction", "eligible", "chosen"})
	for i, s := range rows {
		params, _ := json.Marshal(s.Params)
		w.Write([]string{s.RunID, s.Recommender, string(params), s.Commit, strconv.Itoa(s.Collections),
			strconv.Itoa(s.CollectionDays), fmt.Sprintf("%.5f", s.MeanRelativeSlack),
			fmt.Sprintf("%.5f", s.ExceedanceWindowsPerCollectionDay), fmt.Sprintf("%.5f", s.LimitChangesPerCollectionDay),
			fmt.Sprintf("%.4f", s.ExceedanceFreeCollectionDayFraction), mark(eligible[i]), mark(chosen[s.Recommender] == i && eligible[i])})
		fmt.Printf("%-32s slack %.4f exceed/day %.4f changes/day %.4f %s%s\n", s.RunID, s.MeanRelativeSlack,
			s.ExceedanceWindowsPerCollectionDay, s.LimitChangesPerCollectionDay, mark(eligible[i]), map[bool]string{true: " chosen"}[chosen[s.Recommender] == i && eligible[i]])
	}
	w.Flush()
	if math.IsNaN(float64(rows[0].MeanRelativeSlack)) {
		return fmt.Errorf("no scored collections")
	}
	return w.Error()
}

func mark(b bool) string {
	if b {
		return "yes"
	}
	return ""
}
