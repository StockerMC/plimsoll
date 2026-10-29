package window

import (
	"encoding/json"
	"fmt"
	"time"
)

type state struct {
	Usage []float64
	Wsum  float64
	Mean  float64
	Peaks []float64
	Last  time.Time
	Raw   float64
	Limit float64
	Ring  []float64
}

// SaveState returns the statistics, for a checkpoint of the ensemble that
// holds this recommender as an arm.
func (r *Recommender) SaveState() (json.RawMessage, error) {
	return json.Marshal(state{Usage: r.usage, Wsum: r.wsum, Mean: r.mean, Peaks: r.peaks, Last: r.last, Raw: r.raw, Limit: r.limit, Ring: r.ring})
}

// LoadState checks a saved state and returns the function that installs it.
func (r *Recommender) LoadState(raw json.RawMessage) (func(), error) {
	var st state
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	switch {
	case len(st.Usage) != len(r.usage):
		return nil, fmt.Errorf("%d buckets, want %d", len(st.Usage), len(r.usage))
	case len(st.Peaks) > max(r.opts.PeakWindows, 1), len(st.Ring) > max(r.opts.MaxWindows, 1):
		return nil, fmt.Errorf("%d peaks and %d limits, want at most %d and %d", len(st.Peaks), len(st.Ring), max(r.opts.PeakWindows, 1), max(r.opts.MaxWindows, 1))
	}
	return func() {
		r.usage, r.wsum, r.mean, r.peaks, r.last, r.raw, r.limit, r.ring = st.Usage, st.Wsum, st.Mean, st.Peaks, st.Last, st.Raw, st.Limit, st.Ring
	}, nil
}
