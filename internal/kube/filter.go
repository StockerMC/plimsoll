package kube

import (
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// DefaultRecommenderName is the name of upstream's recommender. VPAs with an
// empty spec.recommenders belong to it and to nobody else.
const DefaultRecommenderName = "default"

// FilterVPAs keeps the VPAs that any of the named recommenders handles,
// following filterVPAs in upstream pkg/recommender/input/cluster_feeder.go.
func FilterVPAs(vpas []unstructured.Unstructured, recommenders ...string) []unstructured.Unstructured {
	var handled []unstructured.Unstructured
	for _, vpa := range vpas {
		if _, ok := SelectedRecommender(&vpa, recommenders...); ok {
			handled = append(handled, vpa)
		}
	}
	return handled
}

// SelectedRecommender returns the first of the named recommenders that the
// VPA selects.
func SelectedRecommender(vpa *unstructured.Unstructured, recommenders ...string) (string, bool) {
	names := recommenderNames(vpa)
	for _, r := range recommenders {
		if len(names) == 0 && r == DefaultRecommenderName || slices.Contains(names, r) {
			return r, true
		}
	}
	return "", false
}

func recommenderNames(vpa *unstructured.Unstructured) []string {
	selectors, _, _ := unstructured.NestedSlice(vpa.Object, "spec", "recommenders")
	var names []string
	for _, s := range selectors {
		if name, ok := s.(map[string]any)["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}
