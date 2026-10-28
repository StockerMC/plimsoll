package main

import (
	"strings"
	"testing"
)

func TestSettle(t *testing.T) {
	tests := []struct {
		name   string
		csv    string
		side   int
		settle int
	}{
		{
			name: "settles once within 10% of the final target",
			csv: `elapsed_s,event,ours_target,upstream_target,ours_restarts,ours_reason,upstream_restarts,upstream_reason
0,,,,0,,0,
15,,250Mi,,0,,0,
30,,400Mi,,0,,0,
45,,430Mi,,0,,0,
60,,440Mi,,0,,0,`,
			settle: 30,
		},
		{
			name: "counts from the last event",
			csv: `elapsed_s,event,ours_target,upstream_target,ours_restarts,ours_reason,upstream_restarts,upstream_reason
0,,440Mi,,0,,0,
15,restart,440Mi,,0,,0,
30,,250Mi,,0,,0,
45,,440Mi,,0,,0,`,
			settle: 30,
		},
		{
			name: "already settled at the event",
			csv: `elapsed_s,event,ours_target,upstream_target,ours_restarts,ours_reason,upstream_restarts,upstream_reason
0,,440Mi,440Mi,0,,0,
15,metrics-down,440Mi,440Mi,0,,0,
30,,440Mi,440Mi,0,,0,`,
			side:   1,
			settle: 0,
		},
		{
			name: "a gap in the target breaks the run",
			csv: `elapsed_s,event,ours_target,upstream_target,ours_restarts,ours_reason,upstream_restarts,upstream_reason
0,,440Mi,,0,,0,
15,,,,0,,0,
30,,440Mi,,0,,0,`,
			settle: 30,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, series, err := readSeries(strings.NewReader(tt.csv))
			if err != nil {
				t.Fatal(err)
			}
			if got := settle(series, tt.side, 0.10); got != tt.settle {
				t.Errorf("settle = %d, want %d", got, tt.settle)
			}
		})
	}
}

func TestSummarize(t *testing.T) {
	csv := `elapsed_s,event,ours_target,ensemble_target,upstream_target,ours_restarts,ours_reason,ensemble_restarts,ensemble_reason,upstream_restarts,upstream_reason
0,,,,,0,,0,,0,
15,,250Mi,250Mi,,0,,0,,0,
30,,480Mi,500Mi,300Mi,1,OOMKilled,1,OOMKilled,1,Error
45,,450Mi,500Mi,,2,OOMKilled,1,OOMKilled,2,OOMKilled`
	sides, series, err := readSeries(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(sides) != 3 || sides[1] != "ensemble" {
		t.Fatalf("sides = %v, want [ours ensemble upstream]", sides)
	}
	ours := summarize(series, 0, 0.10)
	if ours.final != 450<<20 || ours.min != 250<<20 || ours.max != 480<<20 || ours.oomKills != 2 || ours.settle != 30 {
		t.Errorf("ours = %+v", ours)
	}
	ensemble := summarize(series, 1, 0.10)
	if ensemble.final != 500<<20 || ensemble.min != 250<<20 || ensemble.oomKills != 1 || ensemble.settle != 30 {
		t.Errorf("ensemble = %+v", ensemble)
	}
	upstream := summarize(series, 2, 0.10)
	if upstream.final != 0 || upstream.settle != -1 || upstream.oomKills != 1 {
		t.Errorf("upstream = %+v", upstream)
	}
}
