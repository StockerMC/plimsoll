package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A comparison must refuse runs that scored different collections or days
// instead of silently pairing what overlaps.
func TestCompareRejectsMismatchedRuns(t *testing.T) {
	const header = "collection_id,group,windows,days,mean_slack,memory_time,exceedances,changes\n"
	base := header + "1,g1,288,1,0.2,10,0,1\n2,g2,576,2,0.3,20,1,0\n"
	tests := []struct {
		name, candidate string
		ok              bool
	}{
		{"same population", header + "1,g1,288,1,0.1,9,0,1\n2,g2,576,2,0.25,18,0,0\n", true},
		{"missing collection", header + "1,g1,288,1,0.1,9,0,1\n", false},
		{"other collection", header + "1,g1,288,1,0.1,9,0,1\n3,g2,576,2,0.25,18,0,0\n", false},
		{"different group", header + "1,g1,288,1,0.1,9,0,1\n2,g3,576,2,0.25,18,0,0\n", false},
		{"different days", header + "1,g1,288,1,0.1,9,0,1\n2,g2,576,1,0.25,18,0,0\n", false},
		{"duplicate id", header + "1,g1,288,1,0.1,9,0,1\n1,g1,288,1,0.1,9,0,1\n2,g2,576,2,0.25,18,0,0\n", false},
		{"malformed value", header + "1,g1,288,1,0.1,9,0,x\n2,g2,576,2,0.25,18,0,0\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeRunFiles(t, filepath.Join(dir, "cand"), "cand", tt.candidate)
			writeRunFiles(t, filepath.Join(dir, "base"), "base", base)
			err := compareRuns(filepath.Join(dir, "cand"), filepath.Join(dir, "base"), filepath.Join(dir, "out"))
			if (err == nil) != tt.ok {
				t.Errorf("err = %v, want ok %v", err, tt.ok)
			}
		})
	}
}

func writeRunFiles(t *testing.T, dir, id, collections string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	summary := `{"run_id":"` + id + `","actuator":"every","split":"report"}`
	for name, body := range map[string]string{"summary.json": summary, "collections.csv": collections} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
