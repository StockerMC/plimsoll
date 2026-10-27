package kube

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var podMetricsResource = schema.GroupVersionResource{Group: "metrics.k8s.io", Version: "v1beta1", Resource: "pods"}

// MemoryUsage is one metrics-server reading of a container's memory in bytes.
type MemoryUsage struct {
	Pod       string
	Container string
	Bytes     float64
	Time      time.Time
}

// ListMemoryUsage returns the latest reading of every container of the pods
// in namespace that match the label selector.
func ListMemoryUsage(ctx context.Context, client dynamic.Interface, namespace, selector string) ([]MemoryUsage, error) {
	list, err := client.Resource(podMetricsResource).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("list pod metrics in %s: %w", namespace, err)
	}
	var usage []MemoryUsage
	for _, pm := range list.Items {
		stamp, _, _ := unstructured.NestedString(pm.Object, "timestamp")
		at, err := time.Parse(time.RFC3339, stamp)
		if err != nil {
			return nil, fmt.Errorf("pod metrics %s timestamp %q: %w", pm.GetName(), stamp, err)
		}
		containers, _, _ := unstructured.NestedSlice(pm.Object, "containers")
		for _, c := range containers {
			m, _ := c.(map[string]any)
			name, _, _ := unstructured.NestedString(m, "name")
			memory, _, _ := unstructured.NestedString(m, "usage", "memory")
			q, err := resource.ParseQuantity(memory)
			if err != nil {
				return nil, fmt.Errorf("pod metrics %s container %s memory %q: %w", pm.GetName(), name, memory, err)
			}
			usage = append(usage, MemoryUsage{Pod: pm.GetName(), Container: name, Bytes: float64(q.Value()), Time: at})
		}
	}
	return usage, nil
}
