package kube

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var podResource = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

// OOM is one OOMKilled container, with the memory request it was running
// under in bytes.
type OOM struct {
	Pod       string
	Container string
	Request   float64
	Time      time.Time
}

// OOMDetector finds OOM kills by comparing each container's restartCount
// with the count seen on the previous scan. This is the pod-status half of
// upstream pkg/recommender/input/oom/observer.go without the informer.
type OOMDetector struct {
	restarts map[string]int32
}

// NewOOMDetector returns a detector with no restart counts seen.
func NewOOMDetector() *OOMDetector {
	return &OOMDetector{restarts: make(map[string]int32)}
}

// Scan returns the containers whose restartCount rose since the last scan
// and whose last termination was OOMKilled. A container seen for the first
// time sets the baseline, so kills from before the recommender started are
// not replayed. Pods missing from pods are forgotten.
func (d *OOMDetector) Scan(pods []corev1.Pod) []OOM {
	var ooms []OOM
	seen := make(map[string]int32)
	for _, pod := range pods {
		for _, status := range pod.Status.ContainerStatuses {
			key := pod.Namespace + "/" + pod.Name + "/" + status.Name
			seen[key] = status.RestartCount
			last, ok := d.restarts[key]
			if !ok || status.RestartCount <= last {
				continue
			}
			t := status.LastTerminationState.Terminated
			if t == nil || t.Reason != "OOMKilled" {
				continue
			}
			ooms = append(ooms, OOM{
				Pod:       pod.Name,
				Container: status.Name,
				Request:   memoryRequest(&pod, status.Name),
				Time:      t.FinishedAt.Time,
			})
		}
	}
	d.restarts = seen
	return ooms
}

func memoryRequest(pod *corev1.Pod, container string) float64 {
	for _, c := range pod.Spec.Containers {
		if c.Name == container {
			return float64(c.Resources.Requests.Memory().Value())
		}
	}
	return 0
}

// ListPods returns the pods in namespace that match the label selector.
func ListPods(ctx context.Context, client dynamic.Interface, namespace, selector string) ([]corev1.Pod, error) {
	list, err := client.Resource(podResource).Namespace(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("list pods in %s: %w", namespace, err)
	}
	pods := make([]corev1.Pod, len(list.Items))
	for i, item := range list.Items {
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &pods[i]); err != nil {
			return nil, fmt.Errorf("convert pod %s: %w", item.GetName(), err)
		}
	}
	return pods, nil
}
