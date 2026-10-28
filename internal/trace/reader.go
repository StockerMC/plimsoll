package trace

import (
	"compress/gzip"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

// Window is one instance_usage row: one instance over up to five minutes.
type Window struct {
	Start, End time.Time
	Instance   int
	AvgMem     float64
	MaxMem     float64
	Assigned   float64
}

// Update is one instance event that changed the instance's memory request.
type Update struct {
	Time     time.Time
	Instance int
	ReqMem   float64
}

// Collection is one sampled job: its usage rows, the request each instance
// had at the trace start, and its request updates, all in time order.
type Collection struct {
	ID              int64
	VerticalScaling int
	User            string
	LogicalName     string
	Windows         []Window
	// Memory request per instance from its last event at the trace start.
	Requests map[int]float64
	Updates  []Update
	// Rows dropped for a missing or zero max_mem.
	Skipped int
}

// Vertical scaling values of collection_events.
const (
	Off            = 1
	FullyAutomated = 3
)

type meta struct {
	verticalScaling int
	user, logical   string
}

// Reader streams the sampled collections of an extract in collection order.
type Reader struct {
	sample map[int64]meta
	usage  *table
	events *table
}

// Open reads the sample table and opens the usage and events tables in dir.
// prefix is "a_" for the cell a extract and "" for the test fixtures.
func Open(dir, prefix string) (*Reader, error) {
	sample, err := readSample(filepath.Join(dir, prefix+"sample.csv.gz"))
	if err != nil {
		return nil, err
	}
	usage, err := openTable(filepath.Join(dir, prefix+"usage.csv.gz"), 7)
	if err != nil {
		return nil, err
	}
	events, err := openTable(filepath.Join(dir, prefix+"events.csv.gz"), 5)
	if err != nil {
		usage.Close()
		return nil, err
	}
	return &Reader{sample: sample, usage: usage, events: events}, nil
}

// Next returns the next collection with usage rows, or io.EOF.
func (r *Reader) Next() (Collection, error) {
	first, err := r.usage.peek()
	if err != nil {
		return Collection{}, err
	}
	id := first.id
	m, ok := r.sample[id]
	if !ok {
		return Collection{}, fmt.Errorf("collection %d has usage rows but is not in the sample", id)
	}
	c := Collection{ID: id, VerticalScaling: m.verticalScaling, User: m.user, LogicalName: m.logical}
	for {
		row, err := r.usage.peek()
		if err == io.EOF || (err == nil && row.id != id) {
			break
		}
		if err != nil {
			return Collection{}, err
		}
		r.usage.next()
		w, ok, err := usageRow(row.fields)
		if err != nil {
			return Collection{}, fmt.Errorf("collection %d: %w", id, err)
		}
		if !ok {
			c.Skipped++
			continue
		}
		c.Windows = append(c.Windows, w)
	}
	events, err := r.events.collect(id)
	if err != nil {
		return Collection{}, err
	}
	c.Requests, c.Updates = diff(events)
	return c, nil
}

// Close closes the underlying files.
func (r *Reader) Close() error {
	return errors.Join(r.usage.Close(), r.events.Close())
}

type row struct {
	id     int64
	fields []string
}

// table is a gzip CSV sorted by its first column, read with one row of
// lookahead.
type table struct {
	f       *os.File
	z       *gzip.Reader
	r       *csv.Reader
	pending *row
	done    bool
}

func openTable(path string, fields int) (*table, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	z, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, err
	}
	r := csv.NewReader(z)
	r.FieldsPerRecord = fields
	r.ReuseRecord = true
	if _, err := r.Read(); err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: header: %w", path, err)
	}
	return &table{f: f, z: z, r: r}, nil
}

func (t *table) peek() (*row, error) {
	if t.pending != nil {
		return t.pending, nil
	}
	if t.done {
		return nil, io.EOF
	}
	fields, err := t.r.Read()
	if err == io.EOF {
		t.done = true
		return nil, io.EOF
	}
	if err != nil {
		return nil, err
	}
	id, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return nil, err
	}
	t.pending = &row{id, slices.Clone(fields)}
	return t.pending, nil
}

func (t *table) next() {
	t.pending = nil
}

// collect returns the rows of collection id, skipping rows of earlier ones.
func (t *table) collect(id int64) ([][]string, error) {
	var rows [][]string
	for {
		r, err := t.peek()
		if err == io.EOF || (err == nil && r.id > id) {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		t.next()
		if r.id == id {
			rows = append(rows, r.fields)
		}
	}
}

func (t *table) Close() error {
	return errors.Join(t.z.Close(), t.f.Close())
}

func readSample(path string) (map[int64]meta, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	rows, err := csv.NewReader(z).ReadAll()
	if err != nil {
		return nil, err
	}
	sample := make(map[int64]meta, len(rows))
	for _, r := range rows[1:] {
		id, err := strconv.ParseInt(r[0], 10, 64)
		if err != nil {
			return nil, err
		}
		vs, err := strconv.Atoi(r[3])
		if err != nil {
			return nil, err
		}
		sample[id] = meta{vs, r[4], r[5]}
	}
	return sample, nil
}

// usageRow parses collection_id, instance_index, start_time, end_time,
// avg_mem, max_mem, assigned_memory. ok is false for a missing or zero
// max_mem, which the trace documents as possible and carries no peak.
func usageRow(f []string) (w Window, ok bool, err error) {
	if f[5] == "" {
		return w, false, nil
	}
	ints := [3]int64{}
	for i, s := range f[1:4] {
		if ints[i], err = strconv.ParseInt(s, 10, 64); err != nil {
			return w, false, err
		}
	}
	floats := [3]float64{}
	for i, s := range f[4:7] {
		if s == "" {
			continue
		}
		if floats[i], err = strconv.ParseFloat(s, 64); err != nil {
			return w, false, err
		}
	}
	if floats[1] <= 0 {
		return w, false, nil
	}
	return Window{
		Start:    Time(ints[1]),
		End:      Time(ints[2]),
		Instance: int(ints[0]),
		AvgMem:   floats[0],
		MaxMem:   floats[1],
		Assigned: floats[2],
	}, true, nil
}

// Time converts trace microseconds to a time. The trace's sentinels, 0 for
// before the trace and 2^63-1 for after it, map to the Unix epoch and to a
// time far past the trace.
func Time(us int64) time.Time {
	if us == math.MaxInt64 {
		return time.Unix(us/1e6, 0)
	}
	return time.Unix(us/1e6, us%1e6*1000)
}
