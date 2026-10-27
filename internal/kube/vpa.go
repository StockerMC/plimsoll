package kube

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

var (
	vpaResource        = schema.GroupVersionResource{Group: "autoscaling.k8s.io", Version: "v1", Resource: "verticalpodautoscalers"}
	deploymentResource = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
)

// ListVPAs returns the VPAs in every namespace that the named recommender handles.
func ListVPAs(ctx context.Context, client dynamic.Interface, recommender string) ([]unstructured.Unstructured, error) {
	list, err := client.Resource(vpaResource).Namespace(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list vpas: %w", err)
	}
	return FilterVPAs(list.Items, recommender), nil
}

// TargetContainers returns the container names of the Deployment a VPA points at.
func TargetContainers(ctx context.Context, client dynamic.Interface, vpa *unstructured.Unstructured) ([]string, error) {
	kind, _, _ := unstructured.NestedString(vpa.Object, "spec", "targetRef", "kind")
	name, _, _ := unstructured.NestedString(vpa.Object, "spec", "targetRef", "name")
	if kind != "Deployment" {
		return nil, fmt.Errorf("targetRef kind %q: only Deployment is supported", kind)
	}
	deployment, err := client.Resource(deploymentResource).Namespace(vpa.GetNamespace()).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get deployment %s/%s: %w", vpa.GetNamespace(), name, err)
	}
	containers, _, _ := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
	names := make([]string, 0, len(containers))
	for _, c := range containers {
		if name, ok := c.(map[string]any)["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names, nil
}

// PatchStatus replaces a VPA's status. The CRD serves status as a subresource,
// so the patch has to go through it; against the main resource it is dropped.
func PatchStatus(ctx context.Context, client dynamic.Interface, vpa *unstructured.Unstructured, status Status) error {
	patch, err := json.Marshal([]map[string]any{{"op": "add", "path": "/status", "value": status}})
	if err != nil {
		return fmt.Errorf("marshal status: %w", err)
	}
	_, err = client.Resource(vpaResource).Namespace(vpa.GetNamespace()).
		Patch(ctx, vpa.GetName(), types.JSONPatchType, patch, metav1.PatchOptions{}, "status")
	if err != nil {
		return fmt.Errorf("patch vpa %s/%s status: %w", vpa.GetNamespace(), vpa.GetName(), err)
	}
	return nil
}
