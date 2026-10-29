package kube

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
		Windows:   []recommender.OpenWindow{{Instance: "pod-1", End: at.Add(24 * time.Hour), Peak: 3e8, LastSample: at.Add(2 * time.Minute)}},
		State:     []byte(`{"Limit":4.7645046386109596e+08}`),
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
		!maps.Equal(got.Histogram.BucketWeights, want.Histogram.BucketWeights) || got.Histogram.TotalWeight != want.Histogram.TotalWeight ||
		len(got.Windows) != 1 || got.Windows[0].Instance != "pod-1" || !got.Windows[0].End.Equal(want.Windows[0].End) || got.Windows[0].Peak != want.Windows[0].Peak ||
		string(got.State) != string(want.State) {
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

// A state that takes the object's annotations, with the ones already on it,
// over the limit is refused before anything is written, so the last good
// checkpoint stays.
func TestCheckpointTooLarge(t *testing.T) {
	ctx := context.Background()
	client := newClient()
	at := time.Date(2026, 10, 29, 6, 40, 0, 0, time.UTC)
	small := recommender.Checkpoint{Version: "plimsoll-ensemble-v1", Samples: 1, State: []byte("{}")}
	if err := PutCheckpoint(ctx, client, "ns", "foo", "app", "uid-1/Deployment/foo", small, at); err != nil {
		t.Fatal(err)
	}
	res := client.Resource(checkpointResource).Namespace("ns")
	obj, err := res.Get(ctx, "foo-app", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	annotations := obj.GetAnnotations()
	annotations["other"] = strings.Repeat("0", maxAnnotationBytes/2)
	obj.SetAnnotations(annotations)
	if _, err := res.Update(ctx, obj, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	big := small
	big.Samples = 2
	big.State = bytes.Repeat([]byte("0"), maxAnnotationBytes/2)
	if err := PutCheckpoint(ctx, client, "ns", "foo", "app", "uid-1/Deployment/foo", big, at); !errors.Is(err, ErrCheckpointTooLarge) {
		t.Fatalf("PutCheckpoint: err %v, want ErrCheckpointTooLarge", err)
	}
	if got, _, _ := GetCheckpoint(ctx, client, "ns", "foo", "app", "uid-1/Deployment/foo"); got.Samples != 1 {
		t.Errorf("samples %d after a refused write, want 1", got.Samples)
	}
}
