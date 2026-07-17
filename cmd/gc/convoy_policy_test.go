package main

import (
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/config"
)

func TestValidateConvoyTargetPolicy(t *testing.T) {
	policy := config.ConvoyPolicyConfig{RequireOwnedTarget: true, ForbidDefaultTarget: true}
	tests := []struct {
		name          string
		policy        config.ConvoyPolicyConfig
		owned         bool
		target        string
		defaultBranch string
		wantErr       string
	}{
		{name: "disabled preserves existing behavior", policy: config.ConvoyPolicyConfig{}, owned: true, target: "", defaultBranch: "main"},
		{name: "tracking targetless", policy: policy, owned: false, target: "", defaultBranch: "main"},
		{name: "owned requires explicit target", policy: policy, owned: true, target: "", defaultBranch: "main", wantErr: "owned convoy requires an explicit target"},
		{name: "owned default target rejected", policy: policy, owned: true, target: "main", defaultBranch: "main", wantErr: `target "main" is the owning rig default branch`},
		{name: "owned non-default target accepted", policy: policy, owned: true, target: "feature/release", defaultBranch: "main"},
		{name: "city owned explicit target no default accepted", policy: policy, owned: true, target: "integration/city", defaultBranch: ""},
		{name: "require-only targetless rejected", policy: config.ConvoyPolicyConfig{RequireOwnedTarget: true}, owned: true, target: "", wantErr: "owned convoy requires an explicit target"},
		{name: "forbid-only targetless accepted", policy: config.ConvoyPolicyConfig{ForbidDefaultTarget: true}, owned: true, target: "", defaultBranch: "main"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConvoyTargetPolicy(tt.policy, tt.owned, tt.target, tt.defaultBranch)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateConvoyTargetPolicy() error = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateConvoyTargetPolicy() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}
