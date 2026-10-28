package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/StockerMC/plimsoll/internal/replay"
	"github.com/StockerMC/plimsoll/internal/trace"
)

const (
	curveWindows = 576 // two days of five-minute windows
	curveP95MiB  = 256
)

// curve is the first two days of a collection's per-window peak, the max
// over its instances of max_mem.
type curve struct {
	id              int64
	verticalScaling int
	start           int64
	updates         int
	peaks           []float64
	p95, max        float64
}

// pickCurves writes three curves from the report split to dir: the
// FULLY_AUTOMATED collection with the most request updates in its first two
// days, the first OFF collection, and the highest peak-to-p95 ratio among the
// rest.
func pickCurves(r *trace.Reader, dir string) error {
	var all []curve
	for {
		c, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if replay.Split(c) != "report" {
			continue
		}
		if cv, ok := firstDays(c); ok {
			all = append(all, cv)
		}
	}
	var picks []curve
	pick := func(better func(a, b curve) bool, eligible func(curve) bool) {
		best := -1
		for i, cv := range all {
			if eligible(cv) && (best < 0 || better(cv, all[best])) {
				best = i
			}
		}
		if best >= 0 {
			picks = append(picks, all[best])
			all = slices.Delete(all, best, best+1)
		}
	}
	pick(func(a, b curve) bool { return a.updates > b.updates },
		func(cv curve) bool { return cv.verticalScaling == trace.FullyAutomated && cv.updates > 0 })
	pick(func(a, b curve) bool { return a.id < b.id },
		func(cv curve) bool { return cv.verticalScaling == trace.Off })
	pick(func(a, b curve) bool { return a.max/a.p95 > b.max/b.p95 },
		func(curve) bool { return true })
	if len(picks) != 3 {
		return fmt.Errorf("found %d of 3 curves", len(picks))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, cv := range picks {
		path := filepath.Join(dir, fmt.Sprint(cv.id)+".csv")
		if err := os.WriteFile(path, []byte(cv.csv()), 0o644); err != nil {
			return err
		}
		fmt.Printf("%s: vertical_scaling %d, %d updates, peak/p95 %.3f, peak %.0f MiB\n",
			path, cv.verticalScaling, cv.updates, cv.max/cv.p95, cv.max/cv.p95*curveP95MiB)
	}
	return nil
}

// firstDays returns the collection's curve, or false when its first two days
// have a missing window.
func firstDays(c trace.Collection) (curve, bool) {
	steps := trace.Steps(c)
	if len(steps) < curveWindows {
		return curve{}, false
	}
	steps = steps[:curveWindows]
	if steps[curveWindows-1].Time.Sub(steps[0].Time) != (curveWindows-1)*trace.WindowLength {
		return curve{}, false
	}
	cv := curve{id: c.ID, verticalScaling: c.VerticalScaling, start: steps[0].Time.UnixMicro()}
	end := steps[curveWindows-1].Time.Add(trace.WindowLength)
	for _, u := range c.Updates {
		if u.Time.Before(end) {
			cv.updates++
		}
	}
	for _, s := range steps {
		peak := 0.0
		for _, u := range s.Usage {
			peak = math.Max(peak, u.Max)
		}
		cv.peaks = append(cv.peaks, peak)
	}
	sorted := slices.Sorted(slices.Values(cv.peaks))
	cv.p95 = sorted[int(math.Ceil(0.95*float64(len(sorted))))-1]
	cv.max = sorted[len(sorted)-1]
	return cv, true
}

// csv is one MiB value per window, after a comment line naming the source
// and the scale.
func (cv curve) csv() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# collection %d, vertical_scaling %d, %d windows of 300 s from trace time %d us, max over instances of max_mem, scaled so the p95 is %d MiB\n",
		cv.id, cv.verticalScaling, curveWindows, cv.start, curveP95MiB)
	for _, p := range cv.peaks {
		fmt.Fprintf(&b, "%.1f\n", p/cv.p95*curveP95MiB)
	}
	return b.String()
}
