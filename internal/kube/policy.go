package kube

import (
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// MemoryPolicy is what a VPA's resourcePolicy says about one container's
// memory: the container's own entry, else the "*" entry, as upstream's
// GetContainerResourcePolicy resolves it.
type MemoryPolicy struct {
	// The container's mode is Off or memory is not among its
	// controlledResources, so it gets no recommendation, as upstream's
	// NeedsRecommendation and filterControlledResources decide.
	Skip bool
	// minAllowed and maxAllowed memory in bytes; 0 when unset.
	Min, Max float64
}

// ContainerMemoryPolicy resolves the memory policy of one container of a VPA.
func ContainerMemoryPolicy(vpa *unstructured.Unstructured, container string) (MemoryPolicy, error) {
	policies, _, _ := unstructured.NestedSlice(vpa.Object, "spec", "resourcePolicy", "containerPolicies")
	var own, wildcard map[string]any
	for _, p := range policies {
		m, _ := p.(map[string]any)
		switch m["containerName"] {
		case container:
			own = m
		case "*":
			wildcard = m
		}
	}
	chosen := own
	if chosen == nil {
		chosen = wildcard
	}
	var out MemoryPolicy
	if chosen == nil {
		return out, nil
	}
	if mode, _, _ := unstructured.NestedString(chosen, "mode"); mode == "Off" {
		out.Skip = true
	}
	if controlled, found, _ := unstructured.NestedStringSlice(chosen, "controlledResources"); found && !slices.Contains(controlled, "memory") {
		out.Skip = true
	}
	var err error
	if out.Min, err = allowed(chosen, "minAllowed"); err != nil {
		return MemoryPolicy{}, fmt.Errorf("vpa %s/%s container %s: %w", vpa.GetNamespace(), vpa.GetName(), container, err)
	}
	if out.Max, err = allowed(chosen, "maxAllowed"); err != nil {
		return MemoryPolicy{}, fmt.Errorf("vpa %s/%s container %s: %w", vpa.GetNamespace(), vpa.GetName(), container, err)
	}
	return out, nil
}

func allowed(policy map[string]any, field string) (float64, error) {
	v, found, _ := unstructured.NestedFieldNoCopy(policy, field, "memory")
	if !found {
		return 0, nil
	}
	q, err := resource.ParseQuantity(fmt.Sprint(v))
	if err != nil {
		return 0, fmt.Errorf("%s memory %v: %w", field, v, err)
	}
	return float64(q.Value()), nil
}

// Cap clamps bytes to minAllowed, then to maxAllowed, so maxAllowed wins as
// in upstream's applyVPAPolicyForContainer.
func (p MemoryPolicy) Cap(bytes float64) float64 {
	if p.Min > 0 && bytes < p.Min {
		bytes = p.Min
	}
	if p.Max > 0 && bytes > p.Max {
		bytes = p.Max
	}
	return bytes
}
