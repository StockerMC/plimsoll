package trace

import (
	"io"
	"math"
	"slices"
	"testing"
	"time"
)

func readFixtures(t *testing.T) map[int64]Collection {
	t.Helper()
	r, err := Open("testdata", "")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	all := make(map[int64]Collection)
	for {
		c, err := r.Next()
		if err == io.EOF {
			return all
		}
		if err != nil {
			t.Fatal(err)
		}
		all[c.ID] = c
	}
}

// The fixtures are the first 48 h of three collections (testdata/README.md):
// an OFF job with full windows and no events past the start, a
// FULLY_AUTOMATED job with 17 UPDATE_RUNNING and 2 UPDATE_PENDING changes,
// and an OFF job whose instances restart 55 times under the same index with
// no request change, one row with max_mem empty and one with it zero.
func TestFixtures(t *testing.T) {
	tests := []struct {
		id              int64
		verticalScaling int
		windows         int
		skipped         int
		updates         int
		request         float64
		firstUpdate     time.Time
	}{
		{32345, Off, 577, 0, 0, 0.0130157470703125, time.Time{}},
		{11740794268, FullyAutomated, 577, 0, 19, 0.000377655029296875, Time(25654006698)},
		{41324957395, Off, 577, 2, 0, 0.0546875, time.Time{}},
	}
	all := readFixtures(t)
	if len(all) != len(tests) {
		t.Fatalf("read %d collections, want %d", len(all), len(tests))
	}
	for _, tt := range tests {
		c := all[tt.id]
		steps := Steps(c)
		if c.VerticalScaling != tt.verticalScaling || len(steps) != tt.windows || c.Skipped != tt.skipped || len(c.Updates) != tt.updates {
			t.Errorf("%d: vertical scaling %d, %d windows, %d skipped, %d updates; want %d, %d, %d, %d",
				c.ID, c.VerticalScaling, len(steps), c.Skipped, len(c.Updates), tt.verticalScaling, tt.windows, tt.skipped, tt.updates)
		}
		for i := range 2 {
			start := 0.0
			for _, u := range c.Requests {
				if u.Instance == i && u.Time.Equal(Time(0)) {
					start = u.ReqMem
				}
			}
			if start != tt.request {
				t.Errorf("%d: instance %d starts at request %v, want %v", c.ID, i, start, tt.request)
			}
		}
		if len(c.Updates) > 0 && !c.Updates[0].Time.Equal(tt.firstUpdate) {
			t.Errorf("%d: first update at %v, want %v", c.ID, c.Updates[0].Time, tt.firstUpdate)
		}
		for i := 1; i < len(steps); i++ {
			if !steps[i].Time.After(steps[i-1].Time) {
				t.Fatalf("%d: window %d at %v is not after window %d", c.ID, i, steps[i].Time, i-1)
			}
		}
	}
}

// Instance 1 of 41324957395 restarts twice inside the window starting at
// 1200 s and appears in four rows of 33, 22, 2 and 1 s. The window carries
// its 58 s, the duration-weighted mean of the rows' averages and the largest
// row maximum, and the sample still holds one peak per instance.
func TestShortRowsMergeIntoOneInstanceWindow(t *testing.T) {
	steps := Steps(readFixtures(t)[41324957395])
	var step Step
	for _, s := range steps {
		if s.Time.Equal(Time(1200000000)) {
			step = s
		}
	}
	if len(step.Sample.Peaks) != 2 || len(step.Usage) != 2 {
		t.Fatalf("window at 1200 s: %d peaks, %d usage rows, want 2 and 2", len(step.Sample.Peaks), len(step.Usage))
	}
	var u Usage
	for _, x := range step.Usage {
		if x.Instance == 1 {
			u = x
		}
	}
	if u.Seconds != 58 || math.Abs(u.Avg-0.03251227017106681) > 1e-15 || u.Max != 0.03497314453125 {
		t.Errorf("instance 1: %+v", u)
	}
	if step.Sample.Peaks[1].Instance != "1" || step.Sample.Peaks[1].Peak != u.Max {
		t.Errorf("peak %+v, want instance 1 at %v", step.Sample.Peaks[1], u.Max)
	}
}

// Instance 1 first appears an hour in, and its request changes on a
// SCHEDULE, which is a request change but not a request update.
func TestDiffKeepsRequestChangesOfAnyEvent(t *testing.T) {
	rows := [][]string{
		{"1", "0", "0", "0", "1"},
		{"1", "1", "3600000000", "3", "3"},
		{"1", "1", "7200000000", "3", "4"},
		{"1", "1", "7200000000", "6", "4"},
		{"1", "1", "9000000000", "10", "5"},
		{"1", "0", "9000000000", "10", ""},
	}
	requests, updates := diff(rows)
	want := []Update{{Time(0), 0, 1}, {Time(3600000000), 1, 3}, {Time(7200000000), 1, 4}, {Time(9000000000), 1, 5}}
	if !slices.Equal(requests, want) {
		t.Errorf("requests %v, want %v", requests, want)
	}
	if !slices.Equal(updates, want[3:]) {
		t.Errorf("updates %v, want %v", updates, want[3:])
	}
}
