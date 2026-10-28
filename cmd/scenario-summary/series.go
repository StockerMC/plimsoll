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
	usage    []float64 // trace runs only
}

// readSeries reads one scenario CSV. The header names the sides: elapsed_s,
// event, one <side>_target per side, then <side>_restarts and <side>_reason
// per side. A trace run adds curve_mib and one <side>_usage per side.
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
	width := len(rows[0])
	if n == 0 || (width != 2+3*n && width != 3+4*n) {
		return nil, nil, fmt.Errorf("header %v: want elapsed_s, event, then target, restarts and reason per side", rows[0])
	}
	for _, row := range rows[1:] {
		if len(row) != width {
			return nil, nil, fmt.Errorf("row %v: want %d fields", row, width)
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
		if width > 2+3*n {
			// The curve_mib column is for reading the CSV, not scored.
			if s.usage, err = readUsage(row[3+3*n:]); err != nil {
				return nil, nil, err
			}
		}
		series = append(series, s)
	}
	return sides, series, nil
}

func readUsage(fields []string) ([]float64, error) {
	usage := make([]float64, len(fields))
	for i, field := range fields {
		if field == "" {
			continue
		}
		q, err := resource.ParseQuantity(field)
		if err != nil {
			return nil, fmt.Errorf("usage %q: %w", field, err)
		}
		usage[i] = float64(q.Value())
	}
	return usage, nil
}
