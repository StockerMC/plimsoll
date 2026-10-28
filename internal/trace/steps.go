package trace

import (
	"maps"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/StockerMC/plimsoll/internal/recommender"
)

// WindowLength is the trace's usage window, the paper's cadence.
const WindowLength = 5 * time.Minute

// traceStart is 600 s before the trace's first day, the origin of trace days.
var traceStart = time.Unix(600, 0)

// Usage is one instance over one five-minute window, merged from the rows
// the trace split at restarts and request updates.
type Usage struct {
	Instance int
	Avg      float64 // duration-weighted mean of the rows' averages
	Max      float64
	Seconds  float64
}

// Step is one five-minute window: the sample a recommender observes and the
// usage it is scored on.
type Step struct {
	Time   time.Time
	Day    int
	Sample recommender.Sample
	Usage  []Usage
	// Mean of the rows' assigned_memory, the limit the trace ran with.
	Assigned float64
}

// Steps buckets the collection's rows into five-minute windows with one peak
// per instance.
func Steps(c Collection) []Step {
	byWindow := make(map[int64]*Step)
	for _, w := range c.Windows {
		k := int64(w.Start.Sub(traceStart) / WindowLength)
		s, ok := byWindow[k]
		if !ok {
			start := traceStart.Add(time.Duration(k) * WindowLength)
			s = &Step{Time: start, Day: Day(start)}
			byWindow[k] = s
		}
		s.add(w)
	}
	steps := make([]Step, 0, len(byWindow))
	for _, k := range slices.Sorted(maps.Keys(byWindow)) {
		s := byWindow[k]
		total := 0.0
		for i := range s.Usage {
			u := &s.Usage[i]
			if u.Seconds > 0 {
				u.Avg /= u.Seconds
			}
			s.Sample.Peaks = append(s.Sample.Peaks, recommender.InstancePeak{Instance: strconv.Itoa(u.Instance), Peak: u.Max})
			total += u.Seconds
		}
		if total > 0 {
			s.Assigned /= total
		}
		s.Sample.Time = s.Time
		steps = append(steps, *s)
	}
	return steps
}

func (s *Step) add(w Window) {
	seconds := w.End.Sub(w.Start).Seconds()
	i := -1
	for j := range s.Usage {
		if s.Usage[j].Instance == w.Instance {
			i = j
		}
	}
	if i < 0 {
		s.Usage = append(s.Usage, Usage{Instance: w.Instance})
		i = len(s.Usage) - 1
	}
	u := &s.Usage[i]
	u.Avg += w.AvgMem * seconds
	u.Max = math.Max(u.Max, w.MaxMem)
	u.Seconds += seconds
	s.Assigned += w.Assigned * seconds
}

// Day is the trace day of t, counted from the trace start.
func Day(t time.Time) int {
	d := int(t.Sub(traceStart) / (24 * time.Hour))
	return max(d, 0)
}
