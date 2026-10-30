package kube

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)

var (
	vpaResource        = schema.GroupVersionResource{Group: "autoscaling.k8s.io", Version: "v1", Resource: "verticalpodautoscalers"}
	deploymentResource = schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}
)

// ListVPAs returns the VPAs in namespace that any of the named recommenders
// handles, or in every namespace when namespace is empty.
func ListVPAs(ctx context.Context, client dynamic.Interface, namespace string, recommenders ...string) ([]unstructured.Unstructured, error) {
	list, err := client.Resource(vpaResource).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list vpas: %w", err)
	}
	return FilterVPAs(list.Items, recommenders...), nil
}

// Target is what a VPA points at: the container names of its Deployment and
// the label selector that finds the Deployment's pods.
type Target struct {
	Containers []string
	Selector   string
}

// LookupTarget reads the Deployment a VPA points at.
func LookupTarget(ctx context.Context, client dynamic.Interface, vpa *unstructured.Unstructured) (Target, error) {
	kind, _, _ := unstructured.NestedString(vpa.Object, "spec", "targetRef", "kind")
	name, _, _ := unstructured.NestedString(vpa.Object, "spec", "targetRef", "name")
	if kind != "Deployment" {
		return Target{}, fmt.Errorf("targetRef kind %q: only Deployment is supported", kind)
	}
	deployment, err := client.Resource(deploymentResource).Namespace(vpa.GetNamespace()).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return Target{}, fmt.Errorf("get deployment %s/%s: %w", vpa.GetNamespace(), name, err)
	}
	var selector metav1.LabelSelector
	raw, _, _ := unstructured.NestedMap(deployment.Object, "spec", "selector")
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &selector); err != nil {
		return Target{}, fmt.Errorf("deployment %s/%s selector: %w", vpa.GetNamespace(), name, err)
	}
	parsed, err := metav1.LabelSelectorAsSelector(&selector)
	if err != nil {
		return Target{}, fmt.Errorf("deployment %s/%s selector: %w", vpa.GetNamespace(), name, err)
	}
	var target Target
	target.Selector = parsed.String()
	containers, _, _ := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
	for _, c := range containers {
		if name, ok := c.(map[string]any)["name"].(string); ok {
			target.Containers = append(target.Containers, name)
		}
	}
	return target, nil
}

// PatchStatus replaces a VPA's status. The CRD serves status as a subresource,
// so the patch has to go through it; against the main resource it is dropped.
// The patch fails if the VPA was recreated or changed since it was read, so a
// recommendation computed for the old object is never written to the new one.
func PatchStatus(ctx context.Context, client dynamic.Interface, vpa *unstructured.Unstructured, status Status) error {
	patch, err := json.Marshal([]map[string]any{
		{"op": "test", "path": "/metadata/uid", "value": vpa.GetUID()},
		{"op": "test", "path": "/metadata/resourceVersion", "value": vpa.GetResourceVersion()},
		{"op": "add", "path": "/status", "value": status},
	})
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
