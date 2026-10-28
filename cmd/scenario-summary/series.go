package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

type sample struct {
	elapsed  int
	event    string
	target   []float64
	restarts []int
	reason   []string
}

// readSeries reads one scenario CSV. The header names the sides: elapsed_s,
// event, one <side>_target per side, then <side>_restarts and <side>_reason
// per side.
func readSeries(r io.Reader) (sides []string, series []sample, err error) {
	rows, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 || len(rows[0]) < 2 {
		return nil, nil, fmt.Errorf("no header")
	}
	for _, col := range rows[0][2:] {
		if side, ok := strings.CutSuffix(col, "_target"); ok {
			sides = append(sides, side)
		}
	}
	n := len(sides)
	if n == 0 || len(rows[0]) != 2+3*n {
		return nil, nil, fmt.Errorf("header %v: want elapsed_s, event, then target, restarts and reason per side", rows[0])
	}
	for _, row := range rows[1:] {
		if len(row) != 2+3*n {
			return nil, nil, fmt.Errorf("row %v: want %d fields", row, 2+3*n)
		}
		s := sample{event: row[1], target: make([]float64, n), restarts: make([]int, n), reason: make([]string, n)}
		if s.elapsed, err = strconv.Atoi(row[0]); err != nil {
			return nil, nil, fmt.Errorf("elapsed %q: %w", row[0], err)
		}
		for i := range sides {
			if field := row[2+i]; field != "" {
				q, err := resource.ParseQuantity(field)
				if err != nil {
					return nil, nil, fmt.Errorf("target %q: %w", field, err)
				}
				s.target[i] = float64(q.Value())
			}
			if field := row[2+n+2*i]; field != "" {
				if s.restarts[i], err = strconv.Atoi(field); err != nil {
					return nil, nil, fmt.Errorf("restarts %q: %w", field, err)
				}
			}
			s.reason[i] = row[2+n+2*i+1]
		}
		series = append(series, s)
	}
	return sides, series, nil
}
