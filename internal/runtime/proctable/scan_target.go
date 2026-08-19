package proctable

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gastownhall/gascity/internal/pathutil"
	"github.com/gastownhall/gascity/internal/runtime"
)

func identityObservationError(operation string, cause error) error {
	return fmt.Errorf("%w: %s: %w", runtime.ErrProcessIdentityIncomplete, operation, cause)
}

func processMatchesTarget(env map[string]string, cwd string, argv []string, target runtime.ProcessTarget) bool {
	sessionID := strings.TrimSpace(env["GC_SESSION_ID"])
	if target.SessionID == "" && target.Alias == "" && len(target.ProcessNames) == 0 {
		return sessionID != ""
	}
	if target.SessionID != "" && sessionID == target.SessionID {
		return true
	}
	if sessionID != "" {
		return false
	}
	if target.WorkDir == "" || !pathutil.PathWithin(target.WorkDir, cwd) {
		return false
	}
	if matchingProcessName(argv, target.ProcessNames) == "" {
		return false
	}
	alias := strings.TrimSpace(env["GC_ALIAS"])
	return target.Alias == "" || alias == "" || alias == target.Alias
}

func processNeedsWorkDir(env map[string]string, argv []string, target runtime.ProcessTarget) bool {
	if target.WorkDir == "" || matchingProcessName(argv, target.ProcessNames) == "" {
		return false
	}
	sessionID := strings.TrimSpace(env["GC_SESSION_ID"])
	if target.SessionID != "" && sessionID == target.SessionID && processCity(env) == "" {
		return true
	}
	if sessionID != "" {
		return false
	}
	alias := strings.TrimSpace(env["GC_ALIAS"])
	return target.Alias == "" || alias == "" || alias == target.Alias
}

func processNeedsArgv(env map[string]string, target runtime.ProcessTarget) bool {
	if target.WorkDir == "" || len(target.ProcessNames) == 0 {
		return false
	}
	sessionID := strings.TrimSpace(env["GC_SESSION_ID"])
	return sessionID == "" || target.SessionID != "" && sessionID == target.SessionID && processCity(env) == ""
}

func processCity(env map[string]string) string {
	if city := strings.TrimSpace(env["GC_CITY_PATH"]); city != "" {
		return city
	}
	return strings.TrimSpace(env["GC_CITY"])
}

func matchingProcessName(argv, processNames []string) string {
	for _, name := range processNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		for _, arg := range argv {
			if filepath.Base(arg) == name {
				return name
			}
		}
	}
	return ""
}
