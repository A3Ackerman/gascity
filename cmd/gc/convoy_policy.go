package main

import (
	"path/filepath"
	"strings"

	"github.com/gastownhall/gascity/internal/config"
	convoycore "github.com/gastownhall/gascity/internal/convoy"
)

// validateConvoyTargetPolicy is the CLI-facing policy seam.
func validateConvoyTargetPolicy(policy config.ConvoyPolicyConfig, owned bool, target, defaultBranch string) error {
	return convoycore.ValidateTargetPolicy(policy, owned, target, defaultBranch)
}

// convoyDefaultBranchForBead resolves the owning rig's configured/probed
// default branch. An unscoped city convoy has no rig default and returns empty.
func convoyDefaultBranchForBead(cfg *config.City, cityPath, beadID string) string {
	if cfg == nil {
		return ""
	}
	rig, ok := convoyRigForBead(cfg, beadID)
	if !ok {
		return ""
	}
	if branch := rig.EffectiveDefaultBranch(); branch != "" {
		return branch
	}
	if strings.TrimSpace(rig.Path) == "" {
		return ""
	}
	path := rig.Path
	if cityPath != "" && !filepath.IsAbs(path) {
		path = filepath.Join(cityPath, path)
	}
	return defaultBranchForRig(rig.Name, cfg.Rigs, path)
}

func convoyRigForBead(cfg *config.City, beadID string) (config.Rig, bool) {
	return findRigByPrefix(cfg, beadPrefix(cfg, beadID))
}

func convoyDefaultBranchForSling(cfg *config.City, cityPath, beadID string, agent config.Agent) string {
	if branch := convoyDefaultBranchForBead(cfg, cityPath, beadID); branch != "" {
		return branch
	}
	if cfg == nil {
		return ""
	}
	for _, rig := range cfg.Rigs {
		if rig.Name != agent.Dir {
			continue
		}
		if branch := rig.EffectiveDefaultBranch(); branch != "" {
			return branch
		}
		if strings.TrimSpace(rig.Path) == "" {
			return defaultBranchFor(cityPath)
		}
		path := rig.Path
		if cityPath != "" && !filepath.IsAbs(path) {
			path = filepath.Join(cityPath, path)
		}
		branch := defaultBranchForRig(rig.Name, cfg.Rigs, path)
		if strings.TrimSpace(branch) == "" {
			return "main"
		}
		return branch
	}
	return ""
}

func firstConvoyIssue(args []string) string {
	if len(args) > 1 {
		return args[1]
	}
	return ""
}
