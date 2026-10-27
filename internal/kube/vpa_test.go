package kube

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/fake"
)

func newClient(objects ...runtime.Object) dynamic.Interface {
	listKinds := map[schema.GroupVersionResource]string{vpaResource: "VerticalPodAutoscalerList"}
	return fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), listKinds, objects...)
}

func newDeployment(namespace, name string, containers ...string) *unstructured.Unstructured {
	specs := make([]any, 0, len(containers))
	for _, c := range containers {
		specs = append(specs, map[string]any{"name": c, "image": "pause"})
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"namespace": namespace, "name": name},
		"spec":       map[string]any{"template": map[string]any{"spec": map[string]any{"containers": specs}}},
	}}
}

func TestListVPAs(t *testing.T) {
	ours := newVPA("default", "demo", "plimsoll")
	upstream := newVPA("default", "demo-default")
	other := newVPA("other", "perf", "performance")
	client := newClient(&ours, &upstream, &other)

	got, err := ListVPAs(context.Background(), client, "plimsoll")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].GetName() != "demo" {
		t.Fatalf("got %d vpas, want only demo", len(got))
	}
}

func TestTargetContainers(t *testing.T) {
	vpa := newVPA("default", "demo", "plimsoll")
	client := newClient(&vpa, newDeployment("default", "demo", "app", "sidecar"))

	got, err := TargetContainers(context.Background(), client, &vpa)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "app" || got[1] != "sidecar" {
		t.Fatalf("got containers %v, want [app sidecar]", got)
	}
}

func TestPatchStatus(t *testing.T) {
	vpa := newVPA("default", "demo", "plimsoll")
	client := newClient(&vpa)
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	status := ProvidedStatus(&vpa, Recommendation{ContainerRecommendations: []ContainerRecommendation{{
		ContainerName: "app",
		Target:        corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
		LowerBound:    corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("200Mi")},
		UpperBound:    corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
	}}}, now)

	if err := PatchStatus(context.Background(), client, &vpa, status); err != nil {
		t.Fatal(err)
	}
	got, err := client.Resource(vpaResource).Namespace("default").Get(context.Background(), "demo", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	recs, _, _ := unstructured.NestedSlice(got.Object, "status", "recommendation", "containerRecommendations")
	if len(recs) != 1 {
		t.Fatalf("got %d container recommendations, want 1", len(recs))
	}
	for field, want := range map[string]string{"target": "256Mi", "lowerBound": "200Mi", "upperBound": "1Gi"} {
		memory, _, _ := unstructured.NestedString(recs[0].(map[string]any), field, "memory")
		if memory != want {
			t.Errorf("%s memory %q, want %s", field, memory, want)
		}
	}
	generation, _, _ := unstructured.NestedInt64(got.Object, "status", "observedGeneration")
	if generation != 1 {
		t.Errorf("observedGeneration %d, want 1", generation)
	}
	conditions, _, _ := unstructured.NestedSlice(got.Object, "status", "conditions")
	if len(conditions) != 1 {
		t.Fatalf("got %d conditions, want 1", len(conditions))
	}
	if c := conditions[0].(map[string]any); c["type"] != "RecommendationProvided" || c["status"] != "True" {
		t.Errorf("condition %v, want RecommendationProvided True", c)
	}
}

func TestProvidedStatusKeepsTransitionTime(t *testing.T) {
	vpa := newVPA("default", "demo", "plimsoll")
	vpa.Object["status"] = map[string]any{"conditions": []any{map[string]any{
		"type": "RecommendationProvided", "status": "True", "lastTransitionTime": "2026-11-01T00:00:00Z",
	}}}
	later := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

	status := ProvidedStatus(&vpa, Recommendation{}, later)
	if got := status.Conditions[0].LastTransitionTime.Time; !got.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("lastTransitionTime %v, want the existing one kept", got)
	}
}
