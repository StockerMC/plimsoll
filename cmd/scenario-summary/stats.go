package main

import "math"

type stats struct {
	final, min, max float64
	oomKills        int
	settle          int // -1 when there is no final target
}

func summarize(series []sample, side int, tol float64) stats {
	st := stats{min: math.Inf(1), settle: -1}
	last := -1
	for _, s := range series {
		if t := s.target[side]; t > 0 {
			st.min = math.Min(st.min, t)
			st.max = math.Max(st.max, t)
		}
		// A missing sample is skipped rather than read as zero restarts, which
		// would count every earlier kill again at the next sample. Several
		// restarts between two samples all count as the last reason.
		if r := s.restarts[side]; r >= 0 {
			if last >= 0 && s.reason[side] == "OOMKilled" {
				st.oomKills += max(r-last, 0)
			}
			last = r
		}
	}
	if len(series) == 0 || series[len(series)-1].target[side] == 0 {
		st.min = 0
		return st
	}
	st.final = series[len(series)-1].target[side]
	st.settle = settle(series, side, tol)
	return st
}

// settle returns the seconds from the last event, or the first sample when
// there is none, to the first sample after which every target stays within
// tol of the final target.
func settle(series []sample, side int, tol float64) int {
	from := series[0].elapsed
	for _, s := range series {
		if s.event != "" {
			from = s.elapsed
		}
	}
	final := series[len(series)-1].target[side]
	at := series[len(series)-1].elapsed
	for i := len(series) - 1; i >= 0; i-- {
		s := series[i]
		if s.elapsed < from || math.Abs(s.target[side]-final) > tol*final {
			break
		}
		at = s.elapsed
	}
	return at - from
}
