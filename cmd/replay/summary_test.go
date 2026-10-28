package main

import (
	"encoding/json"
	"testing"
)

// A run with no FULLY_AUTOMATED collection, as a --limit or fixture run
// can be, has undefined tracking means; the summary must still encode.
func TestSummaryOfRunWithoutUpdatesEncodes(t *testing.T) {
	p := &pass{runs: []run{{Name: "r"}}, results: [][]result{{{ID: 1, VerticalScaling: 1}}}}
	if _, err := json.Marshal(summarize(p, 0, "r", "", "all")); err != nil {
		t.Error(err)
	}
}
