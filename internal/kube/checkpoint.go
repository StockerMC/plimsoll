package kube

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/StockerMC/plimsoll/internal/histogram"
	"github.com/StockerMC/plimsoll/internal/recommender"
)

var checkpointResource = schema.GroupVersionResource{Group: "autoscaling.k8s.io", Version: "v1", Resource: "verticalpodautoscalercheckpoints"}

// checkpointStatus is the memory half of upstream's
// VerticalPodAutoscalerCheckpointStatus; cpuHistogram is left empty.
type checkpointStatus struct {
	LastUpdateTime    metav1.Time         `json:"lastUpdateTime"`
	Version           string              `json:"version"`
	MemoryHistogram   histogramCheckpoint `json:"memoryHistogram"`
	FirstSampleStart  metav1.Time         `json:"firstSampleStart"`
	LastSampleStart   metav1.Time         `json:"lastSampleStart"`
	TotalSamplesCount int                 `json:"totalSamplesCount"`
}

type histogramCheckpoint struct {
	ReferenceTimestamp metav1.Time    `json:"referenceTimestamp"`
	BucketWeights      map[int]uint32 `json:"bucketWeights"`
	TotalWeight        float64        `json:"totalWeight"`
}

// ErrUnusableCheckpoint means a checkpoint exists but cannot be restored: it
// belongs to another VPA or container, was written for another workload, or
// its status does not decode.
var ErrUnusableCheckpoint = errors.New("unusable checkpoint")

// workloadAnnotation holds the VPA UID and targetRef a checkpoint was written
// for, so a VPA recreated under the same name, or pointed at another
// workload, does not restore the old workload's history.
const workloadAnnotation = "plimsoll/workload"

// windowsAnnotation holds the open aggregation windows as JSON; upstream's
// checkpoint status has no field for them and the CRD prunes unknown ones.
const windowsAnnotation = "plimsoll/open-windows"

// stateAnnotation holds the model state of a recommender other than the
// percentile one, such as the ensemble's selector and arms.
const stateAnnotation = "plimsoll/state"

// maxAnnotationBytes bounds all of a checkpoint object's annotations, ours
// and any others it already has, under the API server's 256 KiB limit.
const maxAnnotationBytes = 192 << 10

// ErrCheckpointTooLarge means a checkpoint's annotations would pass
// maxAnnotationBytes; nothing is written.
var ErrCheckpointTooLarge = errors.New("checkpoint too large")

// checkpointName is upstream's naming, <vpa>-<container>. foo/bar-baz and
// foo-bar/baz share a name, so reads and writes check the spec.
func checkpointName(vpa, container string) string {
	return vpa + "-" + container
}

func checkOwner(obj *unstructured.Unstructured, vpa, container string) error {
	v, _, _ := unstructured.NestedString(obj.Object, "spec", "vpaObjectName")
	c, _, _ := unstructured.NestedString(obj.Object, "spec", "containerName")
	if v != vpa || c != container {
		return fmt.Errorf("%w: %s/%s belongs to %s/%s", ErrUnusableCheckpoint, obj.GetNamespace(), obj.GetName(), v, c)
	}
	return nil
}

// GetCheckpoint reads the checkpoint of one container of a VPA written for
// workload. It returns false when there is none.
func GetCheckpoint(ctx context.Context, client dynamic.Interface, namespace, vpa, container, workload string) (recommender.Checkpoint, bool, error) {
	name := checkpointName(vpa, container)
	obj, err := client.Resource(checkpointResource).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return recommender.Checkpoint{}, false, nil
	}
	if err != nil {
		return recommender.Checkpoint{}, false, fmt.Errorf("get checkpoint %s/%s: %w", namespace, name, err)
	}
	if err := checkOwner(obj, vpa, container); err != nil {
		return recommender.Checkpoint{}, false, err
	}
	if w := obj.GetAnnotations()[workloadAnnotation]; w != workload {
		return recommender.Checkpoint{}, false, fmt.Errorf("%w: %s/%s was written for workload %q, not %q", ErrUnusableCheckpoint, namespace, name, w, workload)
	}
	raw, err := json.Marshal(obj.Object["status"])
	if err != nil {
		return recommender.Checkpoint{}, false, fmt.Errorf("%w: %s/%s: %v", ErrUnusableCheckpoint, namespace, name, err)
	}
	var s checkpointStatus
	if err := json.Unmarshal(raw, &s); err != nil {
		return recommender.Checkpoint{}, false, fmt.Errorf("%w: %s/%s status: %v", ErrUnusableCheckpoint, namespace, name, err)
	}
	var windows []recommender.OpenWindow
	if w := obj.GetAnnotations()[windowsAnnotation]; w != "" {
		if err := json.Unmarshal([]byte(w), &windows); err != nil {
			return recommender.Checkpoint{}, false, fmt.Errorf("%w: %s/%s open windows: %v", ErrUnusableCheckpoint, namespace, name, err)
		}
	}
	return recommender.Checkpoint{
		Version: s.Version,
		Histogram: histogram.Checkpoint{
			ReferenceTimestamp: s.MemoryHistogram.ReferenceTimestamp.Time,
			BucketWeights:      s.MemoryHistogram.BucketWeights,
			TotalWeight:        s.MemoryHistogram.TotalWeight,
		},
		First:   s.FirstSampleStart.Time,
		Last:    s.LastSampleStart.Time,
		Samples: s.TotalSamplesCount,
		Windows: windows,
		State:   []byte(obj.GetAnnotations()[stateAnnotation]),
	}, true, nil
}

// PutCheckpoint creates or replaces the checkpoint of one container of a VPA
// for workload. The CRD has no status subresource, so the whole object is
// updated. It refuses to overwrite a checkpoint of another VPA or container.
func PutCheckpoint(ctx context.Context, client dynamic.Interface, namespace, vpa, container, workload string, c recommender.Checkpoint, now time.Time) error {
	name := checkpointName(vpa, container)
	weights := c.Histogram.BucketWeights
	if weights == nil {
		weights = map[int]uint32{}
	}
	status, err := toMap(checkpointStatus{
		LastUpdateTime: metav1.NewTime(now),
		Version:        c.Version,
		MemoryHistogram: histogramCheckpoint{
			ReferenceTimestamp: metav1.NewTime(c.Histogram.ReferenceTimestamp),
			BucketWeights:      weights,
			TotalWeight:        c.Histogram.TotalWeight,
		},
		FirstSampleStart:  metav1.NewTime(c.First),
		LastSampleStart:   metav1.NewTime(c.Last),
		TotalSamplesCount: c.Samples,
	})
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}
	windows, err := json.Marshal(c.Windows)
	if err != nil {
		return fmt.Errorf("marshal open windows: %w", err)
	}
	annotations := map[string]string{workloadAnnotation: workload, windowsAnnotation: string(windows), stateAnnotation: string(c.State)}
	err = putCheckpoint(ctx, client, namespace, name, vpa, container, annotations, status)
	if apierrors.IsConflict(err) {
		err = putCheckpoint(ctx, client, namespace, name, vpa, container, annotations, status)
	}
	if err != nil {
		return fmt.Errorf("put checkpoint %s/%s: %w", namespace, name, err)
	}
	return nil
}

func putCheckpoint(ctx context.Context, client dynamic.Interface, namespace, name, vpa, container string, annotations map[string]string, status map[string]any) error {
	res := client.Resource(checkpointResource).Namespace(namespace)
	obj, err := res.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		obj = &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": "autoscaling.k8s.io/v1",
			"kind":       "VerticalPodAutoscalerCheckpoint",
			"metadata":   map[string]any{"name": name, "namespace": namespace},
			"spec":       map[string]any{"vpaObjectName": vpa, "containerName": container},
			"status":     status,
		}}
		if err := checkSize(annotations); err != nil {
			return err
		}
		obj.SetAnnotations(annotations)
		_, err = res.Create(ctx, obj, metav1.CreateOptions{})
		return err
	}
	if err != nil {
		return err
	}
	if err := checkOwner(obj, vpa, container); err != nil {
		return err
	}
	merged := obj.GetAnnotations()
	if merged == nil {
		merged = make(map[string]string)
	}
	maps.Copy(merged, annotations)
	if err := checkSize(merged); err != nil {
		return err
	}
	obj.SetAnnotations(merged)
	obj.Object["status"] = status
	_, err = res.Update(ctx, obj, metav1.UpdateOptions{})
	return err
}

func checkSize(annotations map[string]string) error {
	size := 0
	for k, v := range annotations {
		size += len(k) + len(v)
	}
	if size > maxAnnotationBytes {
		return fmt.Errorf("%w: annotations are %d bytes, limit %d", ErrCheckpointTooLarge, size, maxAnnotationBytes)
	}
	return nil
}

func toMap(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	err = json.Unmarshal(raw, &m)
	return m, err
}
