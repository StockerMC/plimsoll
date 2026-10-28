package kube

import "testing"

// One VPA over four containers: a container's own entry overrides "*", Off
// and controlledResources without memory publish nothing, and maxAllowed
// wins over minAllowed.
func TestContainerMemoryPolicy(t *testing.T) {
	vpa := newVPA("default", "demo", "plimsoll")
	vpa.Object["spec"].(map[string]any)["resourcePolicy"] = map[string]any{
		"containerPolicies": []any{
			map[string]any{"containerName": "*", "minAllowed": map[string]any{"memory": "300Mi"}, "maxAllowed": map[string]any{"memory": "1Gi"}},
			map[string]any{"containerName": "app", "maxAllowed": map[string]any{"memory": "512Mi"}},
			map[string]any{"containerName": "sidecar", "mode": "Off"},
			map[string]any{"containerName": "cpu-only", "controlledResources": []any{"cpu"}},
		},
	}
	const mi = 1 << 20
	tests := []struct {
		container     string
		skip          bool
		bytes, capped float64
	}{
		{"app", false, 900 * mi, 512 * mi},
		{"app", false, 100 * mi, 100 * mi},
		{"other", false, 100 * mi, 300 * mi},
		{"other", false, 2048 * mi, 1024 * mi},
		{"sidecar", true, 0, 0},
		{"cpu-only", true, 0, 0},
	}
	for _, tt := range tests {
		p, err := ContainerMemoryPolicy(&vpa, tt.container)
		if err != nil {
			t.Fatal(err)
		}
		if p.Skip != tt.skip {
			t.Errorf("%s: skip %v, want %v", tt.container, p.Skip, tt.skip)
		}
		if !tt.skip && p.Cap(tt.bytes) != tt.capped {
			t.Errorf("%s: Cap(%v) = %v, want %v", tt.container, tt.bytes, p.Cap(tt.bytes), tt.capped)
		}
	}
	if p := (MemoryPolicy{Min: 600 * mi, Max: 500 * mi}); p.Cap(100*mi) != 500*mi {
		t.Errorf("min above max: Cap = %v, want maxAllowed", p.Cap(100*mi))
	}
}
