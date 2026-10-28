package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/StockerMC/plimsoll/internal/metrics"
	"github.com/StockerMC/plimsoll/internal/recommender"
	"github.com/StockerMC/plimsoll/internal/replay"
	"github.com/StockerMC/plimsoll/internal/trace"
)

var lags = []int{0, 1, 2, 12}

// run is one recommender under one actuation rule.
type run struct {
	Name        string
	Recommender string
	Actuator    string
	Params      Params
	newRec      func() recommender.Recommender
	newAct      replay.NewActuator
}

// result is one run's score on one collection.
type result struct {
	ID              int64
	Group           string
	VerticalScaling int
	Windows         int
	Skipped         int
	metrics.Summary
	Tracking metrics.Tracking
}

// pass replays every collection once through every run.
type pass struct {
	runs    []run
	split   string
	limit   int
	rawDir  string
	results [][]result
	seconds float64
}

func (p *pass) execute(r *trace.Reader) error {
	start := time.Now()
	jobs := make(chan trace.Collection, 2)
	type scored struct {
		run int
		result
	}
	out := make(chan scored, 64)
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range jobs {
				for i, r := range p.runs {
					res, err := p.score(r, c)
					if err != nil {
						fmt.Fprintln(os.Stderr, err)
						continue
					}
					out <- scored{i, res}
				}
			}
		}()
	}
	done := make(chan struct{})
	p.results = make([][]result, len(p.runs))
	go func() {
		for s := range out {
			p.results[s.run] = append(p.results[s.run], s.result)
		}
		close(done)
	}()
	var readErr error
	n := 0
	for p.limit == 0 || n < p.limit {
		c, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			readErr = err
			break
		}
		if p.split != "all" && replay.Split(c) != p.split {
			continue
		}
		jobs <- c
		n++
	}
	close(jobs)
	wg.Wait()
	close(out)
	<-done
	for _, rs := range p.results {
		slices.SortFunc(rs, func(a, b result) int { return int(a.ID - b.ID) })
	}
	p.seconds = time.Since(start).Seconds()
	return readErr
}

func (p *pass) score(r run, c trace.Collection) (result, error) {
	s := replay.Run(c, r.newRec, r.newAct)
	res := result{
		ID:              c.ID,
		Group:           replay.Group(c),
		VerticalScaling: c.VerticalScaling,
		Windows:         len(s.Points),
		Skipped:         c.Skipped,
		Summary:         metrics.Summarize(s),
		Tracking:        metrics.Track(s, lags, hourWindows),
	}
	if p.rawDir == "" {
		return res, nil
	}
	dir := filepath.Join(p.rawDir, r.Name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return res, err
	}
	return res, writeRaw(filepath.Join(dir, strconv.FormatInt(c.ID, 10)+".csv"), s)
}

// writeRaw writes the series: one row per window from the first scored one.
func writeRaw(path string, s metrics.Series) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Write([]string{"time_us", "limit", "request", "instances", "p95_avg", "max", "exceeded"})
	for _, p := range s.Points[s.From:] {
		avgs := make([]float64, 0, len(p.Usage))
		peak, exceeded := 0.0, 0
		for _, u := range p.Usage {
			avgs = append(avgs, u.Avg)
			peak = max(peak, u.Max)
			if u.Exceeded {
				exceeded++
			}
		}
		w.Write([]string{
			strconv.FormatInt(p.Time.UnixMicro(), 10),
			strconv.FormatFloat(p.Limit, 'g', -1, 64),
			strconv.FormatFloat(p.Request, 'g', -1, 64),
			strconv.Itoa(len(p.Usage)),
			strconv.FormatFloat(metrics.Percentile(avgs, 0.95), 'g', -1, 64),
			strconv.FormatFloat(peak, 'g', -1, 64),
			strconv.Itoa(exceeded),
		})
	}
	w.Flush()
	return w.Error()
}
