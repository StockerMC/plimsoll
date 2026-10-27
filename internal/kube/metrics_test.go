package kube

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func newPodMetrics(namespace, pod, stamp string, labels map[string]any, memory map[string]string) *unstructured.Unstructured {
	containers := make([]any, 0, len(memory))
	for name, m := range memory {
		containers = append(containers, map[string]any{"name": name, "usage": map[string]any{"cpu": "0", "memory": m}})
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "metrics.k8s.io/v1beta1",
		"kind":       "PodMetrics",
		"metadata":   map[string]any{"namespace": namespace, "name": pod, "labels": labels},
		"timestamp":  stamp,
		"window":     "15s",
		"containers": containers,
	}}
}

// The fake tracker files PodMetrics under a resource guessed from the kind,
// so the objects are added under the real resource name by hand.
func TestListMemoryUsage(t *testing.T) {
	client := newClient()
	for _, pm := range []*unstructured.Unstructured{
		newPodMetrics("default", "demo-1", "2026-11-10T12:00:00Z", map[string]any{"app": "demo"}, map[string]string{"app": "204Ki"}),
		newPodMetrics("default", "other-1", "2026-11-10T12:00:00Z", map[string]any{"app": "other"}, map[string]string{"app": "1Gi"}),
	} {
		if err := client.Tracker().Create(podMetricsResource, pm, "default"); err != nil {
			t.Fatal(err)
		}
	}
	got, err := ListMemoryUsage(context.Background(), client, "default", "app=demo")
	if err != nil {
		t.Fatal(err)
	}
	want := MemoryUsage{Pod: "demo-1", Container: "app", Bytes: 204 * 1024, Time: time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)}
	if len(got) != 1 || !got[0].Time.Equal(want.Time) || got[0].Pod != want.Pod || got[0].Container != want.Container || got[0].Bytes != want.Bytes {
		t.Fatalf("got %+v, want [%+v]", got, want)
	}
}
