package main

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender/percentile"
)

var (
	vpaResource = schema.GroupVersionResource{Group: "autoscaling.k8s.io", Version: "v1", Resource: "verticalpodautoscalers"}
	podResource = schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	podMetrics  = schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods"}
)

func demoPod(t *testing.T, restarts int32) *unstructured.Unstructured {
	status := corev1.ContainerStatus{Name: "app", RestartCount: restarts}
	if restarts > 0 {
		status.LastTerminationState.Terminated = &corev1.ContainerStateTerminated{Reason: "OOMKilled", FinishedAt: metav1.Now()}
	}
	pod := corev1.Pod{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "demo-1", Labels: map[string]string{"app": "demo"}},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:      "app",
			Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("300Mi")}},
		}}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{status}},
	}
	obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&pod)
	if err != nil {
		t.Fatal(err)
	}
	return &unstructured.Unstructured{Object: obj}
}

// An OOM kill while metrics-server is down must still raise the target, and
// the pass must still report the metrics error.
func TestOOMWhileMetricsAreDown(t *testing.T) {
	vpa := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "autoscaling.k8s.io/v1",
		"kind":       "VerticalPodAutoscaler",
		"metadata":   map[string]any{"namespace": "default", "name": "demo", "uid": "uid-1"},
		"spec": map[string]any{
			"targetRef":    map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": "demo"},
			"recommenders": []any{map[string]any{"name": "plimsoll"}},
		},
	}}
	deployment := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]any{"namespace": "default", "name": "demo"},
		"spec": map[string]any{
			"selector": map[string]any{"matchLabels": map[string]any{"app": "demo"}},
			"template": map[string]any{"spec": map[string]any{"containers": []any{map[string]any{"name": "app"}}}},
		},
	}}
	client := fake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		vpaResource: "VerticalPodAutoscalerList",
		podResource: "PodList",
		podMetrics:  "PodMetricsList",
	}, vpa, deployment, demoPod(t, 0))
	client.PrependReactor("list", "pods", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if a.GetResource() == podMetrics {
			return true, nil, errors.New("metrics unavailable")
		}
		return false, nil, nil
	})
	buckets, err := histogram.NewExponentialHistogramOptions(1e12, 1e7, 1.05, 1e-4)
	if err != nil {
		t.Fatal(err)
	}
	l := &loop{
		client: client,
		name:   "plimsoll",
		window: 5 * time.Minute,
		opts:   percentile.Options{Buckets: buckets, SamplesPerDay: 288, MinMemory: 250 << 20},
		vpas:   make(map[types.UID]*vpaState),
	}
	ctx := context.Background()
	if err := l.runOnce(ctx); err == nil {
		t.Fatal("first pass: want the metrics error")
	}
	if err := client.Tracker().Update(podResource, demoPod(t, 1), "default"); err != nil {
		t.Fatal(err)
	}
	if err := l.runOnce(ctx); err == nil {
		t.Fatal("second pass: want the metrics error")
	}
	got, err := client.Resource(vpaResource).Namespace("default").Get(ctx, "demo", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	recs, _, _ := unstructured.NestedSlice(got.Object, "status", "recommendation", "containerRecommendations")
	if len(recs) != 1 {
		t.Fatalf("got %d container recommendations, want 1", len(recs))
	}
	target, _, _ := unstructured.NestedString(recs[0].(map[string]any), "target", "memory")
	// 300Mi request plus upstream's 100 MiB bump, before the safety margin.
	if q := resource.MustParse(target); q.Value() < 400<<20 {
		t.Errorf("target %s after an OOM at 300Mi, want at least 400Mi", target)
	}
}
