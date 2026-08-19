package proctable

import "strings"

func darwinPSCommand(fields []string) string {
	if len(fields) < 3 {
		return ""
	}
	return fields[2]
}

func darwinPSArgv(fields []string) []string {
	if len(fields) < 3 {
		return nil
	}
	argv := fields[2:]
	for i, field := range argv {
		if looksLikeEnvAssignment(field) {
			return argv[:i]
		}
	}
	return argv
}

func looksLikeEnvAssignment(field string) bool {
	key, _, ok := strings.Cut(field, "=")
	if !ok || key == "" {
		return false
	}
	for i := range len(key) {
		b := key[i]
		if b != '_' && (b < 'A' || b > 'Z') && (b < 'a' || b > 'z') && (i <= 0 || b < '0' || b > '9') {
			return false
		}
	}
	return true
}

func isInfrastructureCommand(command string) bool {
	return strings.Contains(strings.ToLower(command), "tmux")
}
