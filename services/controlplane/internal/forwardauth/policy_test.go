package forwardauth

import (
	"net/http"
	"testing"

	"appliance-code/services/controlplane/internal/appliance"
	"appliance-code/services/controlplane/internal/roles"
)

func TestRequiredPermissionMCP(t *testing.T) {
	decision := RequiredPermission("appliance.example.internal", http.MethodPost, "/mcp")
	if !decision.Allowed {
		t.Fatalf("decision.Allowed = false, want true")
	}
	if decision.Permission != roles.PermMCPInvoke {
		t.Errorf("permission = %q, want %q", decision.Permission, roles.PermMCPInvoke)
	}
	if decision.Capability != appliance.CapabilityBase {
		t.Errorf("capability = %q, want %q", decision.Capability, appliance.CapabilityBase)
	}
}

func TestRequiredPermissionRegistry(t *testing.T) {
	tests := []struct {
		method      string
		want        string
		wantAllowed bool
	}{
		{method: http.MethodGet, want: roles.PermRegistryPull, wantAllowed: true},
		{method: http.MethodHead, want: roles.PermRegistryPull, wantAllowed: true},
		{method: http.MethodPost, want: roles.PermRegistryPush, wantAllowed: true},
		{method: http.MethodPut, want: roles.PermRegistryPush, wantAllowed: true},
		{method: http.MethodPatch, want: roles.PermRegistryPush, wantAllowed: true},
		{method: http.MethodDelete, want: roles.PermRegistryDelete, wantAllowed: true},
		{method: http.MethodOptions, wantAllowed: false},
	}

	for _, tc := range tests {
		t.Run(tc.method, func(t *testing.T) {
			decision := RequiredPermission("registry.example.internal", tc.method, "/v2/library/nginx/manifests/latest")
			if decision.Allowed != tc.wantAllowed {
				t.Fatalf("Allowed = %v, want %v", decision.Allowed, tc.wantAllowed)
			}
			if tc.wantAllowed && decision.Permission != tc.want {
				t.Errorf("permission = %q, want %q", decision.Permission, tc.want)
			}
			if tc.wantAllowed && decision.Capability != appliance.CapabilityArtifact {
				t.Errorf("capability = %q, want %q", decision.Capability, appliance.CapabilityArtifact)
			}
		})
	}
}

func TestRequiredPermissionFailsClosedForUnknownRoute(t *testing.T) {
	decision := RequiredPermission("grafana.example.internal", http.MethodGet, "/")
	if decision.Allowed {
		t.Fatal("Allowed = true, want false")
	}
	if decision.ReasonCode == "" {
		t.Error("ReasonCode should be set for a denied decision")
	}
}
