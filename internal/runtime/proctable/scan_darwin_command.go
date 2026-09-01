package proctable

import (
	"path/filepath"
	"strings"
)

func darwinPSCommand(fields []string) string {
	if len(fields) < 3 {
		return ""
	}
	return fields[2]
}

// isInfrastructureCommand reports whether a process name is tmux
// infrastructure rather than an agent. Both scanners use it: Darwin passes the
// first token of ps's command column (argv[0], possibly a path), Linux passes
// /proc/<pid>/comm. The match is exact on the path-stripped name — the bare
// executable, or the "tmux: server" / "tmux: client" titles tmux sets through
// setproctitle where the platform supports it. A substring test would also
// hide any tmux-* wrapper that is really an agent root, and a root the scan
// hides is a runtime the orphan sweep can never reap.
func isInfrastructureCommand(command string) bool {
	switch filepath.Base(strings.TrimSpace(command)) {
	case "tmux", "tmux: server", "tmux: client":
		return true
	}
	return false
}
