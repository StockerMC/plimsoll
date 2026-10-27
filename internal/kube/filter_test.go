package kube

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func newVPA(namespace, name string, recommenders ...string) unstructured.Unstructured {
	selectors := make([]any, 0, len(recommenders))
	for _, r := range recommenders {
		selectors = append(selectors, map[string]any{"name": r})
	}
	return unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "autoscaling.k8s.io/v1",
		"kind":       "VerticalPodAutoscaler",
		"metadata":   map[string]any{"namespace": namespace, "name": name, "generation": int64(1)},
		"spec": map[string]any{
			"targetRef":    map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": name},
			"recommenders": selectors,
		},
	}}
}

func TestFilterVPAs(t *testing.T) {
	tests := []struct {
		name         string
		recommender  string
		recommenders []string
		handled      bool
	}{
		{"names us", "plimsoll", []string{"plimsoll"}, true},
		{"empty list", "plimsoll", nil, false},
		{"names default", "plimsoll", []string{"default"}, false},
		{"names someone else", "plimsoll", []string{"performance"}, false},
		{"default handles empty list", "default", nil, true},
		{"default handles its name", "default", []string{"default"}, true},
		{"default ignores other names", "default", []string{"plimsoll"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vpas := []unstructured.Unstructured{newVPA("default", "demo", tt.recommenders...)}
			got := FilterVPAs(vpas, tt.recommender)
			if handled := len(got) == 1; handled != tt.handled {
				t.Errorf("recommender %q with recommenders %v: handled %v, want %v", tt.recommender, tt.recommenders, handled, tt.handled)
			}
		})
	}
}
