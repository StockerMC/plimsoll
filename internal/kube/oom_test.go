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
)

var killedAt = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func newPod(name, request string, restarts int32, reason string) corev1.Pod {
	status := corev1.ContainerStatus{Name: "app", RestartCount: restarts}
	if reason != "" {
		status.LastTerminationState.Terminated = &corev1.ContainerStateTerminated{
			Reason:     reason,
			FinishedAt: metav1.NewTime(killedAt),
		}
	}
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: name, Labels: map[string]string{"app": "demo"}},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "app",
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse(request)},
				Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("1Gi")},
			},
		}}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{status}},
	}
}

func withSidecar(p corev1.Pod) corev1.Pod {
	p.Spec.Containers = append([]corev1.Container{{
		Name: "sidecar",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi")},
		},
	}}, p.Spec.Containers...)
	p.Status.ContainerStatuses = append(p.Status.ContainerStatuses, corev1.ContainerStatus{Name: "sidecar"})
	return p
}

func TestOOMDetectorScan(t *testing.T) {
	tests := []struct {
		name   string
		before corev1.Pod
		after  corev1.Pod
		want   []OOM
	}{
		{
			name:   "restart with OOMKilled records the request, not the 1Gi limit it was killed at",
			before: newPod("demo-1", "256Mi", 0, ""),
			after:  newPod("demo-1", "256Mi", 1, "OOMKilled"),
			want:   []OOM{{Pod: "demo-1", Container: "app", Request: 256 << 20, Time: killedAt}},
		},
		{
			name:   "the killed container's request, not its neighbour's",
			before: withSidecar(newPod("demo-1", "256Mi", 0, "")),
			after:  withSidecar(newPod("demo-1", "256Mi", 1, "OOMKilled")),
			want:   []OOM{{Pod: "demo-1", Container: "app", Request: 256 << 20, Time: killedAt}},
		},
		{
			name:   "restart with another reason records nothing",
			before: newPod("demo-1", "256Mi", 0, ""),
			after:  newPod("demo-1", "256Mi", 1, "Error"),
		},
		{
			name:   "unchanged restartCount records nothing",
			before: newPod("demo-1", "256Mi", 1, "OOMKilled"),
			after:  newPod("demo-1", "256Mi", 1, "OOMKilled"),
		},
		{
			name:   "two restarts between scans record once",
			before: newPod("demo-1", "256Mi", 1, "OOMKilled"),
			after:  newPod("demo-1", "256Mi", 3, "OOMKilled"),
			want:   []OOM{{Pod: "demo-1", Container: "app", Request: 256 << 20, Time: killedAt}},
		},
		{
			name:   "a new pod sets the baseline",
			before: newPod("demo-1", "256Mi", 0, ""),
			after:  newPod("demo-2", "256Mi", 2, "OOMKilled"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewOOMDetector()
			if got := d.Scan([]corev1.Pod{tt.before}); len(got) != 0 {
				t.Fatalf("first scan got %+v, want nothing", got)
			}
			got := d.Scan([]corev1.Pod{tt.after})
			if len(got) != len(tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got %+v, want %+v", got[i], tt.want[i])
				}
			}
		})
	}
}

func TestListPods(t *testing.T) {
	var objects []runtime.Object
	for _, p := range []corev1.Pod{newPod("demo-1", "64Mi", 0, ""), newPod("other-1", "64Mi", 0, "")} {
		if p.Name == "other-1" {
			p.Labels = map[string]string{"app": "other"}
		}
		obj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&p)
		if err != nil {
			t.Fatal(err)
		}
		u := &unstructured.Unstructured{Object: obj}
		u.SetAPIVersion("v1")
		u.SetKind("Pod")
		objects = append(objects, u)
	}
	pods, err := ListPods(context.Background(), newClient(objects...), "default", "app=demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(pods) != 1 || pods[0].Name != "demo-1" {
		t.Fatalf("got %d pods %+v, want demo-1 only", len(pods), pods)
	}
	if got := pods[0].Spec.Containers[0].Resources.Requests.Memory().Value(); got != 64<<20 {
		t.Errorf("request %d, want %d", got, 64<<20)
	}
}
