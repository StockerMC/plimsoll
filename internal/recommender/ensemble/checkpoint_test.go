package ensemble

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender"
	"github.com/StockerMC/plimsoll/internal/recommender/ml"
)

var t0 = time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)

func options(t *testing.T) Options {
	buckets, err := histogram.NewExponentialHistogramOptions(1e12, 1e7, 1.05, 1e-4)
	if err != nil {
		t.Fatal(err)
	}
	return Options{Buckets: buckets, MinMemory: 250 << 20, LargeLimit: 1 << 30, OOMMinBump: 100 << 20, TimeScale: 60}
}

// event is one window, or an OOM when oom is set.
type event struct {
	at   time.Time
	peak float64
	oom  bool
}

// Plateau past the cold start, a rise, an OOM, a fall long enough to
// release the held value, a three-hour gap, then a second plateau.
func events() []event {
	var es []event
	at := t0
	add := func(n int, peak float64) {
		for range n {
			es = append(es, event{at: at, peak: peak})
			at = at.Add(5 * time.Minute)
		}
	}
	add(30, 4e8)
	add(10, 6e8)
	es = append(es, event{at: at, peak: 6e8, oom: true})
	add(30, 2e8)
	at = at.Add(3 * time.Hour)
	add(60, 3e8)
	return es
}

func apply(s *ml.Selector, e event) {
	if e.oom {
		s.RecordOOM("a", e.at, e.peak)
		return
	}
	s.Observe(recommender.Sample{Time: e.at, Peaks: []recommender.InstancePeak{{Instance: "a", Peak: e.peak}, {Instance: "b", Peak: e.peak / 2}}})
}

func restart(t *testing.T, s *ml.Selector, o Options) *ml.Selector {
	c, err := s.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	r := New(o)
	if err := r.Restore(c); err != nil {
		t.Fatal(err)
	}
	return r
}

// A selector restored from a checkpoint, once or before every event, must
// recommend exactly what an uninterrupted one does: pending OOM bumps, a
// partly elapsed fall hold and the arms' histograms all carry over, and a
// window seen before the restart is not learned twice.
func TestRestoreContinuesLikeUninterrupted(t *testing.T) {
	plain := options(t)
	held := plain
	held.FallFraction, held.FallWindows = 0.1, 6
	ratio := held
	ratio.RatioWindows, ratio.RatioFloor = 8, 1.05
	for _, tt := range []struct {
		name string
		o    Options
	}{{"plain", plain}, {"held", held}, {"ratio", ratio}} {
		for _, every := range []int{1, 7, 45} {
			ref, s := New(tt.o), New(tt.o)
			top, fell := 0.0, false
			for i, e := range events() {
				if i%every == 0 {
					s = restart(t, s, tt.o)
					if i > 0 && !events()[i-1].oom {
						apply(s, events()[i-1])
					}
				}
				apply(ref, e)
				apply(s, e)
				got, want := s.Recommend(), ref.Recommend()
				if got != want || s.Limit() != ref.Limit() {
					t.Fatalf("%s, restart every %d: event %d: target %v limit %v, want %v %v", tt.name, every, i, got.Target, s.Limit(), want.Target, ref.Limit())
				}
				top = max(top, want.Target)
				fell = fell || want.Target < top
			}
			if !fell {
				t.Errorf("%s: the target never fell, so the fall hold was not exercised", tt.name)
			}
		}
	}
}

// A checkpoint that does not fit is refused whole: the selector that
// refused it saves the same state as before.
func TestRestoreRefusesWithoutChange(t *testing.T) {
	o := options(t)
	src := New(o)
	for _, e := range events()[:20] {
		apply(src, e)
	}
	good, err := src.Checkpoint()
	if err != nil {
		t.Fatal(err)
	}
	other := o
	other.FallWindows = 6
	edit := func(f func(st map[string]any)) recommender.Checkpoint {
		var st map[string]any
		if err := json.Unmarshal(good.State, &st); err != nil {
			t.Fatal(err)
		}
		f(st)
		c := good
		c.State, err = json.Marshal(st)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	malformed := good
	malformed.State = good.State[:len(good.State)/2]
	for _, tt := range []struct {
		name string
		o    Options
		c    recommender.Checkpoint
	}{
		{"malformed", o, malformed},
		{"other config", other, good},
		{"percentile version", o, recommender.Checkpoint{Version: "plimsoll-v2 aggregation=24h0m0s half-life=24h0m0s", State: good.State}},
		{"missing arm", o, edit(func(st map[string]any) { st["Arms"] = st["Arms"].([]any)[1:] })},
		{"last arm's buckets", o, edit(func(st map[string]any) {
			arms := st["Arms"].([]any)
			arms[len(arms)-1].(map[string]any)["Usage"] = []float64{1}
		})},
	} {
		s := New(tt.o)
		for _, e := range events()[40:50] {
			apply(s, e)
		}
		before, _ := s.Checkpoint()
		if err := s.Restore(tt.c); err == nil {
			t.Errorf("%s: want error", tt.name)
		}
		after, _ := s.Checkpoint()
		if !bytes.Equal(before.State, after.State) {
			t.Errorf("%s: refused checkpoint changed the selector", tt.name)
		}
	}
}
