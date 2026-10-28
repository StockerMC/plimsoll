package kube

import (
	"context"
	"errors"
	"maps"
	"testing"
	"time"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender"
)

// foo/bar-baz and foo-bar/baz share the checkpoint name foo-bar-baz; the
// second must neither read nor overwrite the first's state.
func TestCheckpointRefusesOtherOwner(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	at := time.Date(2026, 10, 28, 6, 40, 0, 0, time.UTC)
	want := recommender.Checkpoint{
		Version:   "plimsoll-v1",
		Histogram: histogram.Checkpoint{ReferenceTimestamp: at.Add(24 * time.Hour), BucketWeights: map[int]uint32{22: 10000}, TotalWeight: 1.5},
		First:     at,
		Last:      at.Add(2 * time.Minute),
		Samples:   2,
	}
	if err := PutCheckpoint(ctx, client, "ns", "foo", "bar-baz", "uid-1/Deployment/foo", want, at); err != nil {
		t.Fatal(err)
	}

	got, found, err := GetCheckpoint(ctx, client, "ns", "foo", "bar-baz", "uid-1/Deployment/foo")
	if err != nil || !found {
		t.Fatalf("GetCheckpoint: found %v, err %v", found, err)
	}
	if got.Version != want.Version || !got.First.Equal(want.First) || !got.Last.Equal(want.Last) || got.Samples != want.Samples ||
		!got.Histogram.ReferenceTimestamp.Equal(want.Histogram.ReferenceTimestamp) ||
		!maps.Equal(got.Histogram.BucketWeights, want.Histogram.BucketWeights) || got.Histogram.TotalWeight != want.Histogram.TotalWeight {
		t.Fatalf("GetCheckpoint = %+v, want %+v", got, want)
	}

	if _, _, err := GetCheckpoint(ctx, client, "ns", "foo-bar", "baz", "uid-2/Deployment/foo-bar"); !errors.Is(err, ErrUnusableCheckpoint) {
		t.Errorf("GetCheckpoint by other owner: err %v, want ErrUnusableCheckpoint", err)
	}
	other := want
	other.Samples = 99
	if err := PutCheckpoint(ctx, client, "ns", "foo-bar", "baz", "uid-2/Deployment/foo-bar", other, at); !errors.Is(err, ErrUnusableCheckpoint) {
		t.Errorf("PutCheckpoint by other owner: err %v, want ErrUnusableCheckpoint", err)
	}
	if got, _, _ := GetCheckpoint(ctx, client, "ns", "foo", "bar-baz", "uid-1/Deployment/foo"); got.Samples != want.Samples {
		t.Errorf("checkpoint overwritten: samples %d, want %d", got.Samples, want.Samples)
	}
}

// A VPA deleted and recreated under the same name must not restore the old
// one's history, and its first save must replace it.
func TestCheckpointOfAnotherWorkload(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	at := time.Date(2026, 10, 28, 6, 40, 0, 0, time.UTC)
	old := recommender.Checkpoint{Version: "plimsoll-v1", Samples: 5, First: at, Last: at}
	if err := PutCheckpoint(ctx, client, "ns", "foo", "app", "uid-1/Deployment/foo", old, at); err != nil {
		t.Fatal(err)
	}
	if _, _, err := GetCheckpoint(ctx, client, "ns", "foo", "app", "uid-2/Deployment/foo"); !errors.Is(err, ErrUnusableCheckpoint) {
		t.Fatalf("GetCheckpoint for the recreated VPA: err %v, want ErrUnusableCheckpoint", err)
	}
	fresh := old
	fresh.Samples = 1
	if err := PutCheckpoint(ctx, client, "ns", "foo", "app", "uid-2/Deployment/foo", fresh, at); err != nil {
		t.Fatal(err)
	}
	got, found, err := GetCheckpoint(ctx, client, "ns", "foo", "app", "uid-2/Deployment/foo")
	if err != nil || !found || got.Samples != 1 {
		t.Errorf("after the recreated VPA's save: samples %d, found %v, err %v, want 1", got.Samples, found, err)
	}
}
