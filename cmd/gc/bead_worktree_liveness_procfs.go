//go:build !darwin

package main

import (
	"os"
	"path/filepath"
	"strconv"
)

// collectLiveWorktreeState walks /proc/<pid>/cwd for every process on the host
// and records their canonical working directories. When the top-level /proc walk
// fails outright it returns scanned=false so the caller fails closed and reaps
// nothing.
//
// Per-process readlink failures are skipped, not fatal: a process may exit
// mid-walk, and a process owned by another user may have a cwd this process
// cannot resolve. The fleet runs every agent as the same user, so agent worktree
// cwds are always visible here; the active-session-directory cross-check plus
// the git-clean and closed-bead gates back-stop any process this scan cannot
// see. The /proc signal protects, it never authorizes a deletion the other gates
// would refuse.
//
// This file is !darwin. macOS has no /proc and uses the lsof-based scanner in
// bead_worktree_liveness_darwin.go (ga-bq84cj).
func collectLiveWorktreeState() liveWorktreeState {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return liveWorktreeState{scanned: false}
	}
	raw := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue // not a PID directory
		}
		link, err := os.Readlink(filepath.Join("/proc", entry.Name(), "cwd"))
		if err != nil || link == "" {
			continue
		}
		raw = append(raw, link)
	}
	return liveWorktreeState{cwds: normalizeLiveCWDs(raw), scanned: true}
}
