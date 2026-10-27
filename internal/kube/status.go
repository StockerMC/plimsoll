package kube

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Status is the part of VerticalPodAutoscalerStatus this recommender writes.
type Status struct {
	Recommendation     Recommendation `json:"recommendation"`
	Conditions         []Condition    `json:"conditions"`
	ObservedGeneration int64          `json:"observedGeneration"`
}

type Recommendation struct {
	ContainerRecommendations []ContainerRecommendation `json:"containerRecommendations"`
}

type ContainerRecommendation struct {
	ContainerName string              `json:"containerName"`
	Target        corev1.ResourceList `json:"target"`
}

type Condition struct {
	Type               string                 `json:"type"`
	Status             corev1.ConditionStatus `json:"status"`
	LastTransitionTime metav1.Time            `json:"lastTransitionTime"`
}

const recommendationProvided = "RecommendationProvided"

// ProvidedStatus builds the status for a VPA that has a recommendation. The
// condition keeps its transition time when it was already true.
func ProvidedStatus(vpa *unstructured.Unstructured, rec Recommendation, now time.Time) Status {
	return Status{
		Recommendation: rec,
		Conditions: []Condition{{
			Type:               recommendationProvided,
			Status:             corev1.ConditionTrue,
			LastTransitionTime: providedSince(vpa, now),
		}},
		ObservedGeneration: vpa.GetGeneration(),
	}
}

func providedSince(vpa *unstructured.Unstructured, now time.Time) metav1.Time {
	conditions, _, _ := unstructured.NestedSlice(vpa.Object, "status", "conditions")
	for _, c := range conditions {
		m, _ := c.(map[string]any)
		if m["type"] != recommendationProvided || m["status"] != string(corev1.ConditionTrue) {
			continue
		}
		if s, ok := m["lastTransitionTime"].(string); ok {
			if t, err := time.Parse(time.RFC3339, s); err == nil {
				return metav1.NewTime(t)
			}
		}
	}
	return metav1.NewTime(now)
}
