package replay

import (
	"maps"
	"time"

	"github.com/StockerMC/plimsoll/internal/metrics"
	"github.com/StockerMC/plimsoll/internal/recommender"
	"github.com/StockerMC/plimsoll/internal/trace"
)

// ColdStart is left unscored at the start of every collection.
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
	req := requests{current: maps.Clone(c.Requests), updates: c.Updates}
	s.From = len(steps)
	for i, step := range steps {
		if s.From == len(steps) && step.Time.Sub(steps[0].Time) >= ColdStart {
			s.From = i
		}
		r := recommender.Recommendation{Target: step.Assigned, Lower: step.Assigned, Upper: step.Assigned}
		if rec != nil {
			r = rec.Recommend()
		}
		p := metrics.Point{Time: step.Time, Day: step.Day, Limit: act.Limit(r, step.Time)}
		p.Request, p.Updated = req.through(step.Time.Add(trace.WindowLength))
		for _, u := range step.Usage {
			p.Usage = append(p.Usage, metrics.Usage{Instance: u.Instance, Avg: u.Avg, Max: u.Max, Seconds: u.Seconds})
		}
		s.Points = append(s.Points, p)
		if rec != nil {
			rec.Observe(step.Sample)
		}
	}
	return s
}

// requests replays the trace's request updates as a step function.
type requests struct {
	current map[int]float64
	updates []trace.Update
}

// through applies the updates before end and returns the mean request over
// the instances and whether any update was applied.
func (r *requests) through(end time.Time) (float64, bool) {
	updated := false
	for len(r.updates) > 0 && r.updates[0].Time.Before(end) {
		r.current[r.updates[0].Instance] = r.updates[0].ReqMem
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
