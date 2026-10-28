package metrics

import "math"

// Tracking compares the limit series with the request the trace recorded.
type Tracking struct {
	// Mean of |limit[t] - request[t-lag]| / request[t-lag] over the scored
	// windows, by lag in windows.
	Error map[int]float64
	// The same over the After windows following each recorded update; NaN
	// when none are scored.
	PostUpdate map[int]float64
	// Fraction of recorded updates after which the limit moved the same
	// way within After windows.
	Agreement float64
	Updates   int
}

// Track scores the series at each lag. after is the window count that
// defines the post-update windows and the direction check.
func Track(s Series, lags []int, after int) Tracking {
	t := Tracking{Error: make(map[int]float64), PostUpdate: make(map[int]float64)}
	post := make([]bool, len(s.Points))
	for u, p := range s.Points {
		if p.Updated {
			for i := u + 1; i <= u+after && i < len(s.Points); i++ {
				post[i] = true
			}
		}
	}
	for _, lag := range lags {
		sum, n, postSum, postN := 0.0, 0, 0.0, 0
		for i := max(s.From, lag); i < len(s.Points); i++ {
			req := s.Points[i-lag].Request
			if req <= 0 {
				continue
			}
			e := math.Abs(s.Points[i].Limit-req) / req
			sum += e
			n++
			if post[i] {
				postSum += e
				postN++
			}
		}
		t.Error[lag] = ratio(sum, n)
		t.PostUpdate[lag] = ratio(postSum, postN)
	}
	agreed := 0
	for u := max(s.From, 1); u < len(s.Points); u++ {
		p := s.Points[u]
		delta := p.Request - s.Points[u-1].Request
		if !p.Updated || delta == 0 {
			continue
		}
		t.Updates++
		for i := u + 1; i <= u+after && i < len(s.Points); i++ {
			if move := s.Points[i].Limit - p.Limit; move != 0 && (move > 0) == (delta > 0) {
				agreed++
				break
			}
		}
	}
	t.Agreement = ratio(float64(agreed), t.Updates)
	return t
}

func ratio(sum float64, n int) float64 {
	if n == 0 {
		return math.NaN()
	}
	return sum / float64(n)
}
