package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/StockerMC/plimsoll/internal/trace"
)

func main() {
	log.SetFlags(0)
	data := flag.String("data", "data/cell-a", "extract directory")
	prefix := flag.String("prefix", "a_", "file name prefix in the extract directory")
	cell := flag.String("cell", "a", "trace cell of the extract, recorded in each summary")
	names := flag.String("recommender", "percentile", "comma-separated list of "+strings.Join(recommenders, ", "))
	actuators := flag.String("actuator", "every", "comma-separated list of every, updater")
	params := flag.String("params", "", "JSON file of parameters for percentile-tuned, ml and ensemble")
	split := flag.String("split", "all", "collections to replay: tune, report or all")
	limit := flag.Int("limit", 0, "stop after this many collections; 0 for all")
	inspect := flag.Int64("inspect", 0, "print one collection's windows and updates and exit")
	out := flag.String("out", "results/summary", "directory for run summaries, or the CSV path with --sweep")
	raw := flag.String("raw", "", "directory for per-collection series, one subdirectory per run; empty writes none")
	runPrefix := flag.String("run-prefix", "cella", "run ids are this, the recommender and the actuator")
	sweep := flag.String("sweep", "", "JSON grid of runs to replay on the tune split, written as one CSV row each")
	compare := flag.Bool("compare", false, "compare two run directories given as arguments")
	flag.Parse()

	if *compare {
		if flag.NArg() != 2 {
			log.Fatal("usage: replay --compare CANDIDATE_DIR BASELINE_DIR")
		}
		if err := compareRuns(flag.Arg(0), flag.Arg(1), *out); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *sweep != "" && *split != "tune" {
		log.Fatal("--sweep runs on the tune split only")
	}
	r, err := trace.Open(*data, *prefix)
	if err != nil {
		log.Fatal(err)
	}
	defer r.Close()
	if *inspect != 0 {
		if err := inspectCollection(r, *inspect); err != nil {
			log.Fatal(err)
		}
		return
	}

	var p Params
	if *params != "" {
		if err := readJSON(*params, &p); err != nil {
			log.Fatal(err)
		}
	}
	var runs []run
	if *sweep != "" {
		runs, err = sweepRuns(*sweep)
	} else {
		runs, err = flagRuns(strings.Split(*names, ","), strings.Split(*actuators, ","), p, *runPrefix)
	}
	if err != nil {
		log.Fatal(err)
	}
	pass := &pass{runs: runs, cell: *cell, split: *split, limit: *limit, rawDir: *raw}
	if err := pass.execute(r); err != nil {
		log.Fatal(err)
	}
	if *sweep != "" {
		if err := writeSweep(*out, pass); err != nil {
			log.Fatal(err)
		}
		return
	}
	commit := gitCommit()
	for i := range runs {
		s := summarize(pass, i, runs[i].Name, commit, *split)
		if err := writeRun(filepath.Join(*out, runs[i].Name), s, pass.results[i]); err != nil {
			log.Fatal(err)
		}
		printSummary(s)
	}
	fmt.Printf("%d runs over %d collections in %.1f s, %.0f windows/s\n", len(runs), len(pass.results[0]), pass.seconds,
		float64(windows(pass)*len(runs))/pass.seconds)
}

func flagRuns(names, actuators []string, p Params, prefix string) ([]run, error) {
	var runs []run
	for _, name := range names {
		newRec, err := newRecommender(name, p)
		if err != nil {
			return nil, err
		}
		for _, act := range actuators {
			newAct, err := newActuator(act)
			if err != nil {
				return nil, err
			}
			runs = append(runs, run{Name: prefix + "-" + name + "-" + act, Recommender: name, Actuator: act, Params: p, newRec: newRec, newAct: newAct})
		}
	}
	return runs, nil
}

func windows(p *pass) int {
	n := 0
	for _, r := range p.results[0] {
		n += r.Windows
	}
	return n
}

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// gitCommit is the revision the binary was built from, not the checkout's
// HEAD at run time, with -dirty when the build had uncommitted changes.
func gitCommit() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, dirty := "unknown", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value[:min(7, len(s.Value))]
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if dirty {
		rev += "-dirty"
	}
	return rev
}

var errNotFound = errors.New("collection not found")
