package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The watch repeats a termination on every update and re-list, and a
// replacement pod starts its restart count over; each kill must count once.
func TestKillsFromWatch(t *testing.T) {
	terms := `workload,uid,container,container_id,reason,finished_at
ours,u1,app,c1,OOMKilled,2026-10-30T00:00:00Z
ours,u1,app,c1,OOMKilled,2026-10-30T00:00:00Z
ours,u1,app,c2,OOMKilled,2026-10-30T00:01:00Z
ours,u2,app,c1,OOMKilled,2026-10-30T00:02:00Z
held,u3,app,c3,Error,2026-10-30T00:02:00Z
held,u3,app,c4,OOMKilled,2026-10-30T00:03:00Z
`
	for _, tt := range []struct {
		name     string
		watch    string
		complete bool
	}{
		{"one watch", "event,time\nstart,1\n", true},
		{"reconnected", "event,time\nstart,1\nexit,5\nstart,6\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "terminations"), 0o755); err != nil {
				t.Fatal(err)
			}
			for name, body := range map[string]string{"c.csv": terms, "c-watch.csv": tt.watch} {
				if err := os.WriteFile(filepath.Join(dir, "terminations", name), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			counts, complete, found, err := kills(dir, "c")
			if err != nil || !found {
				t.Fatalf("found %v, err %v", found, err)
			}
			if counts["ours"] != 3 || counts["held"] != 1 || complete != tt.complete {
				t.Errorf("counts %v complete %v, want ours 3 held 1 complete %v", counts, complete, tt.complete)
			}
		})
	}
}
