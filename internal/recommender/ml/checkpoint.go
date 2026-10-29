package ml

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/StockerMC/plimsoll/internal/recommender"
)

// StatefulArm is an Arm whose state the Selector can checkpoint.
type StatefulArm interface {
	Arm
	SaveState() (json.RawMessage, error)
	// LoadState checks a saved state and returns the function that installs
	// it, so the Selector can check every arm before changing any.
	LoadState(json.RawMessage) (func(), error)
}

var _ recommender.Checkpointer = (*Selector)(nil)

// selectorState is the Selector's checkpoint. JSON writes each float64 in
// the shortest form that parses back to the same bits, so a restored
// selector breaks the arms' ulp ties as the uninterrupted one would.
type selectorState struct {
	Costs   []float64
	Chosen  int
	Limit   float64
	Ring    []float64
	Held    float64
	Below   int
	Seen    int
	Peak    float64
	First   time.Time
	Last    time.Time
	Peaks   map[string]float64
	Pending []recommender.InstancePeak
	Arms    []json.RawMessage
}

// Checkpoint saves the selector and every arm. Options.Version must name
// the configuration, since state saved under one grid is misread under
// another.
func (s *Selector) Checkpoint() (recommender.Checkpoint, error) {
	if s.opts.Version == "" {
		return recommender.Checkpoint{}, errors.New("selector has no checkpoint version")
	}
	st := selectorState{
		Costs: s.costs, Chosen: s.chosen, Limit: s.limit, Ring: s.ring, Held: s.held, Below: s.below,
		Seen: s.seen, Peak: s.peak, First: s.first, Last: s.last, Peaks: s.peaks, Pending: s.pending,
	}
	for i, arm := range s.opts.Arms {
		a, ok := arm.(StatefulArm)
		if !ok {
			return recommender.Checkpoint{}, fmt.Errorf("arm %d has no checkpoint", i)
		}
		raw, err := a.SaveState()
		if err != nil {
			return recommender.Checkpoint{}, fmt.Errorf("arm %d: %w", i, err)
		}
		st.Arms = append(st.Arms, raw)
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return recommender.Checkpoint{}, err
	}
	return recommender.Checkpoint{Version: s.opts.Version, First: s.first, Last: s.last, Samples: s.seen, State: raw}, nil
}

// Restore loads a checkpoint, or returns an error and leaves the selector
// as it was.
func (s *Selector) Restore(c recommender.Checkpoint) error {
	if c.Version != s.opts.Version {
		return fmt.Errorf("checkpoint version %q, want %q", c.Version, s.opts.Version)
	}
	var st selectorState
	if err := json.Unmarshal(c.State, &st); err != nil {
		return fmt.Errorf("selector state: %w", err)
	}
	n := len(s.opts.Arms)
	switch {
	case len(st.Costs) != n || len(st.Arms) != n:
		return fmt.Errorf("selector state has %d costs and %d arms, want %d", len(st.Costs), len(st.Arms), n)
	case st.Chosen < -1 || st.Chosen >= n:
		return fmt.Errorf("selector state chose arm %d of %d", st.Chosen, n)
	case len(st.Ring) > max(s.opts.MaxWindows, 1):
		return fmt.Errorf("selector state has %d ring limits, want at most %d", len(st.Ring), max(s.opts.MaxWindows, 1))
	}
	install := make([]func(), n)
	for i, arm := range s.opts.Arms {
		a, ok := arm.(StatefulArm)
		if !ok {
			return fmt.Errorf("arm %d has no checkpoint", i)
		}
		f, err := a.LoadState(st.Arms[i])
		if err != nil {
			return fmt.Errorf("arm %d: %w", i, err)
		}
		install[i] = f
	}
	for _, f := range install {
		f()
	}
	if st.Peaks == nil {
		st.Peaks = make(map[string]float64)
	}
	s.costs, s.chosen, s.limit, s.ring, s.held, s.below = st.Costs, st.Chosen, st.Limit, st.Ring, st.Held, st.Below
	s.seen, s.peak, s.first, s.last, s.peaks, s.pending = st.Seen, st.Peak, st.First, st.Last, st.Peaks, st.Pending
	return nil
}

type argminState struct {
	Usage  []float64
	Weight float64
	Last   time.Time
	Raw    float64
	Limit  float64
}

// above and below are rebuilt from usage before every read, so they are not
// saved.
func (a *argmin) SaveState() (json.RawMessage, error) {
	return json.Marshal(argminState{Usage: a.usage, Weight: a.weight, Last: a.last, Raw: a.raw, Limit: a.limit})
}

func (a *argmin) LoadState(raw json.RawMessage) (func(), error) {
	var st argminState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	if len(st.Usage) != len(a.usage) {
		return nil, fmt.Errorf("%d buckets, want %d", len(st.Usage), len(a.usage))
	}
	return func() {
		a.usage, a.weight, a.last, a.raw, a.limit = st.Usage, st.Weight, st.Last, st.Raw, st.Limit
	}, nil
}

type ratioState struct {
	Inner json.RawMessage
	Ring  []float64
	Limit float64
}

func (r *ratio) SaveState() (json.RawMessage, error) {
	inner, ok := r.inner.(StatefulArm)
	if !ok {
		return nil, errors.New("inner arm has no checkpoint")
	}
	raw, err := inner.SaveState()
	if err != nil {
		return nil, err
	}
	return json.Marshal(ratioState{Inner: raw, Ring: r.ring, Limit: r.limit})
}

func (r *ratio) LoadState(raw json.RawMessage) (func(), error) {
	inner, ok := r.inner.(StatefulArm)
	if !ok {
		return nil, errors.New("inner arm has no checkpoint")
	}
	var st ratioState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	if len(st.Ring) > r.windows {
		return nil, fmt.Errorf("%d ratios, want at most %d", len(st.Ring), r.windows)
	}
	install, err := inner.LoadState(st.Inner)
	if err != nil {
		return nil, err
	}
	return func() {
		install()
		r.ring, r.limit = st.Ring, st.Limit
	}, nil
}
