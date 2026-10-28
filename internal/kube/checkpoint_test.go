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
	if err := PutCheckpoint(ctx, client, "ns", "foo", "bar-baz", want, at); err != nil {
		t.Fatal(err)
	}

	got, found, err := GetCheckpoint(ctx, client, "ns", "foo", "bar-baz")
	if err != nil || !found {
		t.Fatalf("GetCheckpoint: found %v, err %v", found, err)
	}
	if got.Version != want.Version || !got.First.Equal(want.First) || !got.Last.Equal(want.Last) || got.Samples != want.Samples ||
		!got.Histogram.ReferenceTimestamp.Equal(want.Histogram.ReferenceTimestamp) ||
		!maps.Equal(got.Histogram.BucketWeights, want.Histogram.BucketWeights) || got.Histogram.TotalWeight != want.Histogram.TotalWeight {
		t.Fatalf("GetCheckpoint = %+v, want %+v", got, want)
	}

	if _, _, err := GetCheckpoint(ctx, client, "ns", "foo-bar", "baz"); !errors.Is(err, ErrUnusableCheckpoint) {
		t.Errorf("GetCheckpoint by other owner: err %v, want ErrUnusableCheckpoint", err)
	}
	other := want
	other.Samples = 99
	if err := PutCheckpoint(ctx, client, "ns", "foo-bar", "baz", other, at); !errors.Is(err, ErrUnusableCheckpoint) {
		t.Errorf("PutCheckpoint by other owner: err %v, want ErrUnusableCheckpoint", err)
	}
	if got, _, _ := GetCheckpoint(ctx, client, "ns", "foo", "bar-baz"); got.Samples != want.Samples {
		t.Errorf("checkpoint overwritten: samples %d, want %d", got.Samples, want.Samples)
	}
}
