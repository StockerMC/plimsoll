package main

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// kills counts each OOMKilled termination once per workload, keyed by pod UID,
// container and container ID, from the pod watch a trace run records in
// terminations/<curve>.csv. Complete is false when the watch reconnected,
// since a pod deleted during the gap takes its terminations with it. Found is
// false for runs recorded before the watch existed.
func kills(dir, curve string) (counts map[string]int, complete, found bool, err error) {
	rows, err := readRows(filepath.Join(dir, "terminations", curve+".csv"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, false, nil
	}
	if err != nil {
		return nil, false, false, err
	}
	watch, err := readRows(filepath.Join(dir, "terminations", curve+"-watch.csv"))
	if err != nil {
		return nil, false, false, err
	}
	starts := 0
	for _, w := range watch {
		if w[0] == "start" {
			starts++
		}
	}
	counts = make(map[string]int)
	seen := make(map[[3]string]bool)
	for _, r := range rows {
		if len(r) != 6 {
			return nil, false, false, fmt.Errorf("%s termination %v: want 6 fields", curve, r)
		}
		key := [3]string{r[1], r[2], r[3]}
		if r[4] != "OOMKilled" || seen[key] {
			continue
		}
		seen[key] = true
		counts[r[0]]++
	}
	return counts, starts == 1, true, nil
}

func readRows(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%s: no header", path)
	}
	return rows[1:], nil
}
