package kube

import (
	"slices"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// DefaultRecommenderName is the name of upstream's recommender. VPAs with an
// empty spec.recommenders belong to it and to nobody else.
const DefaultRecommenderName = "default"

// FilterVPAs keeps the VPAs that the named recommender handles, following
// filterVPAs in upstream pkg/recommender/input/cluster_feeder.go.
func FilterVPAs(vpas []unstructured.Unstructured, recommender string) []unstructured.Unstructured {
	var handled []unstructured.Unstructured
	for _, vpa := range vpas {
		if selectsRecommender(&vpa, recommender) {
			handled = append(handled, vpa)
		}
	}
	return handled
}

func selectsRecommender(vpa *unstructured.Unstructured, recommender string) bool {
	names := recommenderNames(vpa)
	if len(names) == 0 {
		return recommender == DefaultRecommenderName
	}
	return slices.Contains(names, recommender)
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
