package metrics

import (
	"math"
	"slices"
	"time"
)

// Usage is one instance over one window.
type Usage struct {
	Instance int
	Avg      float64
	Max      float64
	Seconds  float64
}

// Point is one window: the limit in force for it, the request the trace had
// for it, and the usage observed in it.
type Point struct {
	Time  time.Time
	Day   int
	Limit float64
	// Mean request the trace's instances ran with; 0 when unknown.
	Request float64
	// A recorded request update fell in this window.
	Updated bool
	Usage   []Usage
}

// Series is one collection's windows. Points before From are the cold start
// and are not scored, but the limit held there counts for the first change.
type Series struct {
	Points []Point
	From   int
}

// Summary scores the windows from From on.
type Summary struct {
	Days    int
	Windows int
	// Mean over collection-days of (mean limit - p95 of window averages) / mean limit.
	MeanSlack float64
	// Sum over instance-windows of limit times seconds.
	MemoryTime float64
	// Instance-windows whose peak was above the limit.
	Exceedances int
	Changes     int
	// Collection-days with no exceedance.
	FreeDays int
}

// Summarize scores the series.
func Summarize(s Series) Summary {
	var out Summary
	type day struct {
		limits, avgs []float64
		exceeded     bool
	}
	days := make(map[int]*day)
	var order []int
	for i := s.From; i < len(s.Points); i++ {
		p := s.Points[i]
		d, ok := days[p.Day]
		if !ok {
			d = &day{}
			days[p.Day] = d
			order = append(order, p.Day)
		}
		d.limits = append(d.limits, p.Limit)
		for _, u := range p.Usage {
			d.avgs = append(d.avgs, u.Avg)
			out.MemoryTime += p.Limit * u.Seconds
			if u.Max > p.Limit {
				out.Exceedances++
				d.exceeded = true
			}
		}
		if i > 0 && p.Limit != s.Points[i-1].Limit {
			out.Changes++
		}
		out.Windows++
	}
	slack := 0.0
	for _, k := range order {
		d := days[k]
		limit := mean(d.limits)
		if limit > 0 {
			slack += (limit - Percentile(d.avgs, 0.95)) / limit
		}
		if !d.exceeded {
			out.FreeDays++
		}
	}
	out.Days = len(order)
	if out.Days > 0 {
		out.MeanSlack = slack / float64(out.Days)
	}
	return out
}

// Percentile is the nearest-rank percentile of xs, or 0 when xs is empty.
func Percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := slices.Clone(xs)
	slices.Sort(sorted)
	i := int(math.Ceil(p*float64(len(sorted)))) - 1
	return sorted[min(max(i, 0), len(sorted)-1)]
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}
