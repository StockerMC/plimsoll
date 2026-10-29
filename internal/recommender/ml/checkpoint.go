package ml

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/StockerMC/plimsoll/internal/recommender"
)

// StatefulArm is an Arm whose state the Selector can checkpoint.
type StatefulArm interface {
	Arm
	SaveState() (json.RawMessage, error)
	// LoadState checks a saved state against last, the selector's latest
	// window, and returns the function that installs it, so the Selector
	// can check every arm before changing any.
	LoadState(raw json.RawMessage, last time.Time) (func(), error)
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
	if err := recommender.DecodeState(c.State, &st); err != nil {
		return fmt.Errorf("selector state: %w", err)
	}
	if err := s.check(st, c.Samples); err != nil {
		return fmt.Errorf("selector state: %w", err)
	}
	n := len(s.opts.Arms)
	install := make([]func(), n)
	for i, arm := range s.opts.Arms {
		a, ok := arm.(StatefulArm)
		if !ok {
			return fmt.Errorf("arm %d has no checkpoint", i)
		}
		f, err := a.LoadState(st.Arms[i], st.Last)
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

// check refuses state the selector could not have reached: every count,
// cost and limit non-negative, a choice exactly when a window was seen, a
// ring as long as the windows seen allow ending at the limit, a held value
// no lower than the ring and a fall count short of release.
func (s *Selector) check(st selectorState, samples int) error {
	n, ringMax := len(s.opts.Arms), max(s.opts.MaxWindows, 1)
	peaks := slices.Collect(maps.Values(st.Peaks))
	for _, p := range st.Pending {
		peaks = append(peaks, p.Peak)
	}
	switch {
	case len(st.Costs) != n || len(st.Arms) != n:
		return fmt.Errorf("%d costs and %d arms, want %d", len(st.Costs), len(st.Arms), n)
	case st.Seen < 0 || st.Seen != samples:
		return fmt.Errorf("%d windows seen, checkpoint says %d", st.Seen, samples)
	case st.Chosen < -1 || st.Chosen >= n || (st.Chosen == -1) != (st.Seen == 0):
		return fmt.Errorf("chose arm %d of %d after %d windows", st.Chosen, n, st.Seen)
	case st.First.After(st.Last) || st.Last.IsZero() != (st.Seen == 0):
		return fmt.Errorf("windows from %v to %v after %d seen", st.First, st.Last, st.Seen)
	case !recommender.NonNegative(st.Costs...) || !recommender.NonNegative(st.Ring...) || !recommender.NonNegative(peaks...) ||
		!recommender.NonNegative(st.Limit, st.Held, st.Peak):
		return errors.New("negative cost, limit or peak")
	case len(st.Ring) > ringMax || (st.Seen > 0 && len(st.Ring) != min(st.Seen, ringMax)):
		return fmt.Errorf("%d ring limits after %d windows, want at most %d", len(st.Ring), st.Seen, ringMax)
	case len(st.Ring) > 0 && (st.Ring[len(st.Ring)-1] != st.Limit || st.Held < slices.Max(st.Ring)):
		return fmt.Errorf("held %v and limit %v disagree with the ring", st.Held, st.Limit)
	case st.Below < 0 || (st.Below > 0 && st.Below >= s.opts.FallWindows):
		return fmt.Errorf("fall count %d, release at %d", st.Below, s.opts.FallWindows)
	}
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

func (a *argmin) LoadState(raw json.RawMessage, last time.Time) (func(), error) {
	var st argminState
	if err := recommender.DecodeState(raw, &st); err != nil {
		return nil, err
	}
	switch {
	case len(st.Usage) != len(a.usage):
		return nil, fmt.Errorf("%d buckets, want %d", len(st.Usage), len(a.usage))
	case !st.Last.Equal(last):
		return nil, fmt.Errorf("last window %v, selector's %v", st.Last, last)
	case !recommender.NonNegative(st.Usage...) || !recommender.NonNegative(st.Weight, st.Raw, st.Limit):
		return nil, errors.New("negative weight or limit")
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

func (r *ratio) LoadState(raw json.RawMessage, last time.Time) (func(), error) {
	inner, ok := r.inner.(StatefulArm)
	if !ok {
		return nil, errors.New("inner arm has no checkpoint")
	}
	var st ratioState
	if err := recommender.DecodeState(raw, &st); err != nil {
		return nil, err
	}
	switch {
	case len(st.Ring) > r.windows:
		return nil, fmt.Errorf("%d ratios, want at most %d", len(st.Ring), r.windows)
	case !recommender.NonNegative(st.Ring...) || !recommender.NonNegative(st.Limit):
		return nil, errors.New("negative ratio or limit")
	}
	install, err := inner.LoadState(st.Inner, last)
	if err != nil {
		return nil, err
	}
	return func() {
		install()
		r.ring, r.limit = st.Ring, st.Limit
	}, nil
}
