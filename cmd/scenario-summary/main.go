package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var scenarios = []string{"steady", "burst", "restart", "missing-metrics"}

const tolerance = 0.10

type result struct {
	scenario, side string
	stats
}

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: scenario-summary DIR")
	}
	dir := os.Args[1]
	var results []result
	for _, name := range scenarios {
		f, err := os.Open(filepath.Join(dir, name+".csv"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			log.Fatal(err)
		}
		series, err := readSeries(f)
		f.Close()
		if err != nil {
			log.Fatalf("%s: %v", name, err)
		}
		for i, side := range sides {
			results = append(results, result{name, side, summarize(series, i, tolerance)})
		}
	}
	if err := writeFile(filepath.Join(dir, "summary.csv"), results, writeCSV); err != nil {
		log.Fatal(err)
	}
	if err := writeFile(filepath.Join(dir, "summary.md"), results, writeMarkdown); err != nil {
		log.Fatal(err)
	}
	writeMarkdown(os.Stdout, results)
}

func writeFile(path string, results []result, write func(io.Writer, []result) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := write(f, results); err != nil {
		f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	return f.Close()
}

func columns(r result) []string {
	settle := ""
	if r.settle >= 0 {
		settle = strconv.Itoa(r.settle)
	}
	return []string{r.scenario, r.side, mib(r.final), mib(r.min), mib(r.max), strconv.Itoa(r.oomKills), settle}
}

var header = []string{"scenario", "recommender", "final_target_mib", "min_target_mib", "max_target_mib", "oom_kills", "settle_s"}

func writeCSV(w io.Writer, results []result) error {
	cw := csv.NewWriter(w)
	cw.Write(header)
	for _, r := range results {
		cw.Write(columns(r))
	}
	cw.Flush()
	return cw.Error()
}

func writeMarkdown(w io.Writer, results []result) error {
	row := func(cells []string) string { return "| " + strings.Join(cells, " | ") + " |\n" }
	rule := make([]string, len(header))
	for i := range rule {
		rule[i] = "---"
	}
	text := row(header) + row(rule)
	for _, r := range results {
		text += row(columns(r))
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
