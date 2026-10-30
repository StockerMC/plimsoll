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
		"metadata": map[string]any{"namespace": namespace, "name": name, "generation": int64(1),
			"uid": namespace + "/" + name, "resourceVersion": "1"},
		"spec": map[string]any{
			"targetRef":    map[string]any{"apiVersion": "apps/v1", "kind": "Deployment", "name": name},
			"recommenders": selectors,
		},
	}}
}

func TestSelectedRecommender(t *testing.T) {
	tests := []struct {
		name         string
		ours         []string
		recommenders []string
		want         string
	}{
		{"names us", []string{"plimsoll"}, []string{"plimsoll"}, "plimsoll"},
		{"empty list", []string{"plimsoll"}, nil, ""},
		{"names default", []string{"plimsoll"}, []string{"default"}, ""},
		{"names someone else", []string{"plimsoll"}, []string{"performance"}, ""},
		{"default handles empty list", []string{"default"}, nil, "default"},
		{"default handles its name", []string{"default"}, []string{"default"}, "default"},
		{"default ignores other names", []string{"default"}, []string{"plimsoll"}, ""},
		{"second name", []string{"plimsoll", "plimsoll-ensemble"}, []string{"plimsoll-ensemble"}, "plimsoll-ensemble"},
		{"both names picks the first of ours", []string{"plimsoll", "plimsoll-ensemble"}, []string{"plimsoll-ensemble", "plimsoll"}, "plimsoll"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vpa := newVPA("default", "demo", tt.recommenders...)
			got, ok := SelectedRecommender(&vpa, tt.ours...)
			if got != tt.want || ok != (tt.want != "") {
				t.Errorf("ours %v with recommenders %v: got %q %v, want %q", tt.ours, tt.recommenders, got, ok, tt.want)
			}
			kept := 0
			if tt.want != "" {
				kept = 1
			}
			if got := len(FilterVPAs([]unstructured.Unstructured{vpa}, tt.ours...)); got != kept {
				t.Errorf("FilterVPAs kept %d, want %d", got, kept)
			}
		})
	}
}
