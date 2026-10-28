package replay

import (
	"time"

	"github.com/StockerMC/plimsoll/internal/metrics"
	"github.com/StockerMC/plimsoll/internal/recommender"
	"github.com/StockerMC/plimsoll/internal/trace"
)

// ColdStart is left unscored at the start of every collection, and so is the
// rest of the trace day it ends in, so every scored day is a whole day.
const ColdStart = 48 * time.Hour

// NewActuator builds the actuation rule for one collection from the request
// the trace had in its first window.
type NewActuator func(request float64, at time.Time) Actuator

// Run replays the collection through a fresh recommender. A nil constructor
// scores the trace's own assigned memory as if it were the recommendation.
func Run(c trace.Collection, newRec func() recommender.Recommender, newAct NewActuator) metrics.Series {
	steps := trace.Steps(c)
	var s metrics.Series
	if len(steps) == 0 {
		return s
	}
	var rec recommender.Recommender
	if newRec != nil {
		rec = newRec()
	}
	act := newAct(steps[0].Assigned, steps[0].Time)
	req := requests{current: make(map[int]float64), changes: c.Requests, updates: c.Updates}
	_, own := act.(every)
	s.From = scoredFrom(steps)
	for _, step := range steps {
		r := recommender.Recommendation{Target: step.Assigned, Lower: step.Assigned, Upper: step.Assigned}
		if rec != nil {
			r = rec.Recommend()
		}
		p := metrics.Point{Time: step.Time, Day: step.Day, Limit: act.Limit(r, step.Time)}
		p.Request, p.Updated = req.through(step.Time.Add(trace.WindowLength))
		for _, u := range step.Usage {
			// Under Every the assigned run's limit is each row's own
			// assigned_memory, which the window's mean would blur.
			exceeded := u.Max > p.Limit
			if rec == nil && own {
				exceeded = u.OverAssigned
			}
			p.Usage = append(p.Usage, metrics.Usage{Instance: u.Instance, Avg: u.Avg, Max: u.Max, Seconds: u.Seconds, Exceeded: exceeded})
		}
		s.Points = append(s.Points, p)
		if rec != nil {
			rec.Observe(step.Sample)
		}
	}
	return s
}

// scoredFrom is the first window of the first trace day that starts after the
// cold start.
func scoredFrom(steps []trace.Step) int {
	warm := -1
	for i, step := range steps {
		if step.Time.Sub(steps[0].Time) < ColdStart {
			continue
		}
		if warm < 0 {
			warm = step.Day
			if trace.Day(step.Time.Add(-trace.WindowLength)) < step.Day {
				return i
			}
			continue
		}
		if step.Day > warm {
			return i
		}
	}
	return len(steps)
}

// requests replays the trace's request changes as a step function.
type requests struct {
	current          map[int]float64
	changes, updates []trace.Update
}

// through applies the changes before end and returns the mean request over
// the instances seen so far and whether a request update fell before end.
func (r *requests) through(end time.Time) (float64, bool) {
	for len(r.changes) > 0 && r.changes[0].Time.Before(end) {
		r.current[r.changes[0].Instance] = r.changes[0].ReqMem
		r.changes = r.changes[1:]
	}
	updated := false
	for len(r.updates) > 0 && r.updates[0].Time.Before(end) {
		r.updates = r.updates[1:]
		updated = true
	}
	if len(r.current) == 0 {
		return 0, updated
	}
	sum := 0.0
	for _, v := range r.current {
		sum += v
	}
	return sum / float64(len(r.current)), updated
}
