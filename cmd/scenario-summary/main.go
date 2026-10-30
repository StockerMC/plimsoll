package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

var scenarios = []string{"steady", "burst", "restart", "missing-metrics"}

const tolerance = 0.10

type result struct {
	scenario, side string
	stats
	exceedance
	// The count is a lower bound: the pod watch reconnected during the run.
	killsIncomplete bool
}

func main() {
	trace := flag.Bool("trace", false, "summarize every CSV in DIR as a trace-to-kind run")
	flag.Parse()
	if flag.NArg() != 1 {
		log.Fatal("usage: scenario-summary [--trace] DIR")
	}
	dir := flag.Arg(0)
	names := scenarios
	if *trace {
		paths, err := filepath.Glob(filepath.Join(dir, "*.csv"))
		if err != nil {
			log.Fatal(err)
		}
		names = nil
		for _, p := range paths {
			if name := strings.TrimSuffix(filepath.Base(p), ".csv"); name != "summary" {
				names = append(names, name)
			}
		}
	}
	var results []result
	for _, name := range names {
		f, err := os.Open(filepath.Join(dir, name+".csv"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			log.Fatal(err)
		}
		sides, series, err := readSeries(f)
		f.Close()
		if err != nil {
			log.Fatalf("%s: %v", name, err)
		}
		counts, complete, watched, err := kills(dir, name)
		if err != nil {
			log.Fatal(err)
		}
		for i, side := range sides {
			r := result{scenario: name, side: side, stats: summarize(series, i, tolerance)}
			if *trace {
				if len(series) == 0 || series[0].usage == nil {
					log.Fatalf("%s: no usage columns", name)
				}
				r.exceedance = scoreUsage(series, i)
			}
			if watched {
				r.oomKills, r.killsIncomplete = counts[side], !complete
			}
			results = append(results, r)
		}
	}
	format := scenarioFormat
	if *trace {
		format = traceFormat
	}
	if err := writeFile(filepath.Join(dir, "summary.csv"), results, format, writeCSV); err != nil {
		log.Fatal(err)
	}
	if err := writeFile(filepath.Join(dir, "summary.md"), results, format, writeMarkdown); err != nil {
		log.Fatal(err)
	}
	writeMarkdown(os.Stdout, results, format)
}

type format struct {
	note    string
	header  []string
	columns func(result) []string
}

var scenarioFormat = format{
	note:   fmt.Sprintf("settle_s: seconds from the last event, or the start, until the target stays within %.0f%% of its final value.", tolerance*100),
	header: []string{"scenario", "recommender", "final_target_mib", "min_target_mib", "max_target_mib", "oom_kills", "settle_s"},
	columns: func(r result) []string {
		return []string{r.scenario, r.side, mib(r.final), mib(r.min), mib(r.max), killsText(r), settleText(r)}
	},
}

var traceFormat = format{
	note:   fmt.Sprintf("One row per curve and recommender. scored: 15 s samples with both a target and a `kubectl top` usage; exceedance: scored samples with usage above the target; slack: mean (target - usage) / target over the scored samples. `kubectl top` carries no timestamp and lags the curve by 15 to 60 s, so a target can already include the usage it is scored against: this is a concurrent comparison, not a test of prediction ahead of time. oom_kills: OOMKilled terminations seen by a pod watch, each pod UID, container and container ID counted once, with + when the watch reconnected and may have missed some (runs without a terminations directory: restarts of the first pod sampled with reason OOMKilled); in updateMode Off the pods have no memory limit, so it only shows the curve ran uninterrupted. settle_s: seconds from the start until the target stays within %.0f%% of its final value.", tolerance*100),
	header: []string{"curve", "recommender", "scored", "exceedance", "mean_slack", "oom_kills", "settle_s"},
	columns: func(r result) []string {
		return []string{r.scenario, r.side, strconv.Itoa(r.scored), strconv.Itoa(r.over), strconv.FormatFloat(r.slack, 'f', 4, 64), killsText(r), settleText(r)}
	},
}

func killsText(r result) string {
	if r.killsIncomplete {
		return strconv.Itoa(r.oomKills) + "+"
	}
	return strconv.Itoa(r.oomKills)
}

func settleText(r result) string {
	if r.settle < 0 {
		return ""
	}
	return strconv.Itoa(r.settle)
}

func writeFile(path string, results []result, f format, write func(io.Writer, []result, format) error) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := write(file, results, f); err != nil {
		file.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	return file.Close()
}

func writeCSV(w io.Writer, results []result, f format) error {
	cw := csv.NewWriter(w)
	cw.Write(f.header)
	for _, r := range results {
		cw.Write(f.columns(r))
	}
	cw.Flush()
	return cw.Error()
}

func writeMarkdown(w io.Writer, results []result, f format) error {
	row := func(cells []string) string { return "| " + strings.Join(cells, " | ") + " |\n" }
	text := f.note + "\n\n" + row(f.header) + row(slices.Repeat([]string{"---"}, len(f.header)))
	for _, r := range results {
		text += row(f.columns(r))
	}
	_, err := io.WriteString(w, text)
	return err
}

func mib(bytes float64) string {
	if bytes == 0 {
		return ""
	}
	return strconv.FormatFloat(bytes/(1<<20), 'f', 0, 64)
}
