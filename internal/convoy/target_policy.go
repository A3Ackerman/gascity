package convoy

import (
	"fmt"
	"strings"

	"github.com/gastownhall/gascity/internal/config"
)

// ValidateTargetPolicy enforces the city-owned convoy target policy. Tracking
// convoys remain targetless; an empty default branch means no owning rig was
// resolvable and therefore cannot make an explicit target invalid.
func ValidateTargetPolicy(policy config.ConvoyPolicyConfig, owned bool, target, defaultBranch string) error {
	if !owned || (!policy.RequireOwnedTarget && !policy.ForbidDefaultTarget) {
		return nil
	}
	target = strings.TrimSpace(target)
	defaultBranch = strings.TrimSpace(defaultBranch)
	if policy.RequireOwnedTarget && target == "" {
		return fmt.Errorf("owned convoy requires an explicit target")
	}
	if policy.ForbidDefaultTarget && target != "" && defaultBranch != "" && target == defaultBranch {
		return fmt.Errorf("target %q is the owning rig default branch %q", target, defaultBranch)
	}
	return nil
}
