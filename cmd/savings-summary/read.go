package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

type podSample struct {
	time                         int64
	workload, pod, uid           string
	deleting, ready              bool
	specRequest, specLimit       float64
	appliedRequest, appliedLimit float64
	resizing                     bool
	restarts                     int
}

type deploySample struct {
	time           int64
	workload       string
	desired, ready int
}

type termination struct {
	workload, uid, container, containerID, reason string
	finished                                      int64
}

type run struct {
	info         map[string]string
	switchTime   int64
	pods         []podSample
	deployments  []deploySample
	targets      map[string]float64
	terminations []termination
}

func readRun(dir string) (run, error) {
	r := run{info: map[string]string{}, targets: map[string]float64{}}
	f, err := os.Open(filepath.Join(dir, "run.txt"))
	if err != nil {
		return r, err
	}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), "=")
		r.info[k] = v
	}
	f.Close()
	if r.switchTime, err = strconv.ParseInt(r.info["switch"], 10, 64); err != nil {
		return r, fmt.Errorf("%s: no switch time: %w", dir, err)
	}
	err = eachRow(filepath.Join(dir, "pods.csv"), 13, func(c []string) error {
		p := podSample{workload: c[1], pod: c[2], uid: c[3], deleting: c[4] != "", ready: c[5] == "True",
			resizing: c[10] == "True" || c[11] == "True"}
		var err error
		if p.time, err = strconv.ParseInt(c[0], 10, 64); err != nil {
			return err
		}
		for i, dst := range []*float64{&p.specRequest, &p.specLimit, &p.appliedRequest, &p.appliedLimit} {
			if *dst, err = quantity(c[6+i]); err != nil {
				return err
			}
		}
		p.restarts, _ = strconv.Atoi(c[12])
		r.pods = append(r.pods, p)
		return nil
	})
	if err != nil {
		return r, err
	}
	err = eachRow(filepath.Join(dir, "deployments.csv"), 4, func(c []string) error {
		t, err := strconv.ParseInt(c[0], 10, 64)
		if err != nil {
			return err
		}
		d := deploySample{time: t, workload: c[1]}
		d.desired, _ = strconv.Atoi(c[2])
		d.ready, _ = strconv.Atoi(c[3])
		r.deployments = append(r.deployments, d)
		return nil
	})
	if err != nil {
		return r, err
	}
	err = eachRow(filepath.Join(dir, "vpas.csv"), 4, func(c []string) error {
		v, err := quantity(c[2])
		r.targets[c[1]] = v
		return err
	})
	if err != nil {
		return r, err
	}
	err = eachRow(filepath.Join(dir, "terminations.csv"), 7, func(c []string) error {
		t := termination{workload: c[0], uid: c[1], container: c[2], containerID: c[3], reason: c[4]}
		if c[6] != "" {
			at, err := parseTime(c[6])
			if err != nil {
				return err
			}
			t.finished = at
		}
		r.terminations = append(r.terminations, t)
		return nil
	})
	return r, err
}

func eachRow(path string, fields int, fn func([]string) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	cr := csv.NewReader(f)
	cr.FieldsPerRecord = -1
	rows, err := cr.ReadAll()
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for i, row := range rows[min(1, len(rows)):] {
		if len(row) != fields {
			return fmt.Errorf("%s:%d: %d fields, want %d", path, i+2, len(row), fields)
		}
		if err := fn(row); err != nil {
			return fmt.Errorf("%s:%d: %w", path, i+2, err)
		}
	}
	return nil
}

func quantity(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return 0, err
	}
	return q.AsApproximateFloat64(), nil
}
