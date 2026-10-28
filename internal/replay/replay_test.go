package replay

import (
	"testing"
	"time"

	"github.com/StockerMC/plimsoll/internal/recommender"
	"github.com/StockerMC/plimsoll/internal/trace"
)

var t0 = trace.Time(600_000_000)

// collection is one instance over n full windows from the trace start.
func collection(n int) trace.Collection {
	c := trace.Collection{ID: 1, Requests: map[int]float64{0: 1}}
	for i := range n {
		start := t0.Add(time.Duration(i) * trace.WindowLength)
		c.Windows = append(c.Windows, trace.Window{Start: start, End: start.Add(trace.WindowLength), AvgMem: 0.5, MaxMem: 1, Assigned: 2})
	}
	return c
}

// counter recommends the number of windows it has observed.
type counter int

func (c *counter) Observe(recommender.Sample)               { *c++ }
func (*counter) RecordOOM(string, time.Time, float64) error { return nil }
func (c *counter) Recommend() recommender.Recommendation {
	return recommender.Recommendation{Target: float64(*c)}
}

// The limit scored for window t must be the one returned before window t
// was observed, so it equals t, never t + 1.
func TestLimitIsSetBeforeTheWindowIsObserved(t *testing.T) {
	s := Run(collection(600), func() recommender.Recommender { return new(counter) }, Every)
	for i, p := range s.Points {
		if p.Limit != float64(i) {
			t.Fatalf("window %d scored against %v recommendations", i, p.Limit)
		}
	}
	if s.From != 576 {
		t.Errorf("scoring starts at window %d, want 576 (48 h of 5-minute windows)", s.From)
	}
}

// Replaying the request updates must not change the collection, or the
// second run over it starts from the last request instead of the first.
func TestRunLeavesTheCollectionUnchanged(t *testing.T) {
	c := collection(600)
	c.Updates = []trace.Update{{Time: t0.Add(time.Hour), Instance: 0, ReqMem: 2}}
	first := Run(c, nil, Every)
	second := Run(c, nil, Every)
	if first.Points[0].Request != 1 || second.Points[0].Request != 1 {
		t.Errorf("request before the update: %v then %v, want 1 both times", first.Points[0].Request, second.Points[0].Request)
	}
}

func TestUpdaterRule(t *testing.T) {
	tests := []struct {
		name                 string
		target, lower, upper float64
		after                time.Duration
		want                 float64
	}{
		{"5% drift inside the bounds", 105, 90, 120, 13 * time.Hour, 100},
		{"12% drift before 12 h", 112, 90, 120, 11 * time.Hour, 100},
		{"12% drift after 12 h", 112, 90, 120, 12 * time.Hour, 112},
		{"request outside the bounds", 130, 125, 140, time.Hour, 130},
		{"5% drift with bounds equal to the target", 105, 105, 105, 13 * time.Hour, 100},
		{"no recommendation yet", 0, 0, 0, 13 * time.Hour, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := Updater(100, t0)
			r := recommender.Recommendation{Target: tt.target, Lower: tt.lower, Upper: tt.upper}
			if got := u.Limit(r, t0.Add(tt.after)); got != tt.want {
				t.Errorf("limit %v, want %v", got, tt.want)
			}
		})
	}
}

// The assignment is pinned so a change to the hash or the key is noticed;
// collections of one logical job land on the same side.
func TestSplitIsPinnedAndGroupsByJob(t *testing.T) {
	tests := []struct {
		c    trace.Collection
		want string
	}{
		{trace.Collection{ID: 32345, User: "JBEMyDFTIiNn1p7tUZhesIz1tAMtFYIahY6oWJP5leQ=", LogicalName: "PivE4eStSmbaQXZ7J7d7I+AkS3vJSKAdyFNrhIkzIu4="}, "tune"},
		{trace.Collection{ID: 11740794268, User: "k0Y3bMjk+Kzl3ktAWqdoaj3cPfla13eTpaSqwW37o2I=", LogicalName: "T9BEkBbex+0x8/ur15xhy+4tNo14YZ4G2UQBfwG9zSs="}, "report"},
		{trace.Collection{ID: 7, User: "k0Y3bMjk+Kzl3ktAWqdoaj3cPfla13eTpaSqwW37o2I=", LogicalName: "T9BEkBbex+0x8/ur15xhy+4tNo14YZ4G2UQBfwG9zSs="}, "report"},
		{trace.Collection{ID: 41324957395, User: "u"}, "report"},
	}
	for _, tt := range tests {
		if got := Split(tt.c); got != tt.want {
			t.Errorf("%d: %s, want %s", tt.c.ID, got, tt.want)
		}
	}
}
