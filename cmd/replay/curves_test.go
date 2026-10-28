package main

import (
	"testing"
	"time"

	"github.com/StockerMC/plimsoll/internal/trace"
)

func TestFirstDays(t *testing.T) {
	start := time.Unix(600, 0)
	collection := func(n int, skip int) trace.Collection {
		var c trace.Collection
		for k := range n {
			if k == skip {
				continue
			}
			at := start.Add(time.Duration(k) * trace.WindowLength)
			peak := float64(k + 1)
			if k >= curveWindows {
				peak = 1e9
			}
			c.Windows = append(c.Windows,
				trace.Window{Start: at, End: at.Add(trace.WindowLength), Instance: 1, MaxMem: peak},
				trace.Window{Start: at, End: at.Add(trace.WindowLength), Instance: 2, MaxMem: 0.5})
		}
		return c
	}
	tests := []struct {
		name     string
		c        trace.Collection
		ok       bool
		p95, max float64
	}{
		// p95 of 1..576 is the 548th value; windows after the first 576 are ignored.
		{"first two days only", collection(600, -1), true, 548, 576},
		{"gap inside the first two days", collection(600, 100), false, 0, 0},
		{"shorter than two days", collection(curveWindows-1, -1), false, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cv, ok := firstDays(tt.c)
			if ok != tt.ok || cv.p95 != tt.p95 || cv.max != tt.max {
				t.Errorf("got ok %v, p95 %g, max %g; want %v, %g, %g", ok, cv.p95, cv.max, tt.ok, tt.p95, tt.max)
			}
		})
	}
}
