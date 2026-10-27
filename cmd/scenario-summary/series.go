package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"

	"k8s.io/apimachinery/pkg/api/resource"
)

var sides = []string{"ours", "upstream"}

type sample struct {
	elapsed  int
	event    string
	target   [2]float64
	restarts [2]int
	reason   [2]string
}

func readSeries(r io.Reader) ([]sample, error) {
	rows, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no header")
	}
	var series []sample
	for _, row := range rows[1:] {
		if len(row) != 8 {
			return nil, fmt.Errorf("row %v: want 8 fields", row)
		}
		s := sample{event: row[1], reason: [2]string{row[5], row[7]}}
		if s.elapsed, err = strconv.Atoi(row[0]); err != nil {
			return nil, fmt.Errorf("elapsed %q: %w", row[0], err)
		}
		for i, field := range []string{row[2], row[3]} {
			if field == "" {
				continue
			}
			q, err := resource.ParseQuantity(field)
			if err != nil {
				return nil, fmt.Errorf("target %q: %w", field, err)
			}
			s.target[i] = float64(q.Value())
		}
		for i, field := range []string{row[4], row[6]} {
			if field == "" {
				continue
			}
			if s.restarts[i], err = strconv.Atoi(field); err != nil {
				return nil, fmt.Errorf("restarts %q: %w", field, err)
			}
		}
		series = append(series, s)
	}
	return series, nil
}
