package proctable

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// LiveTmuxServerPIDs returns the PIDs of every running tmux SERVER process on
// this host.
//
// A tmux server is identified by three properties together, which is what
// separates it from a transient tmux CLIENT carrying identical argv: its
// command is a tmux invocation, it is its own process-group leader (the server
// setsid()s when it daemonizes), and it has been reparented to init. A client
// that merely asks an existing server to do something is a short-lived child of
// gc and matches neither of the last two.
//
// It deliberately enumerates servers on ALL sockets rather than resolving one
// socket's server via `tmux -L <socket> display -p '#{pid}'`. The authoritative
// per-socket lookup needs a socket name, and the deep kill paths do not have
// one; enumerating is also strictly safer, since it protects a neighboring
// city's server too. Failure to run ps returns nil, which degrades to unguarded
// behavior rather than blocking a kill.
//
// This lives in proctable, not in the tmux package, because proctable is the
// package that owns reading the process table AND the package that owns
// KillByPID — the kill path that actually took the city down (hq-nie0). The
// tmux package delegates here so there is one detector, not two that drift.
func LiveTmuxServerPIDs() map[int]bool {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,pgid=,command=").Output()
	if err != nil {
		return nil
	}
	servers := make(map[int]bool)
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, pgid := fields[1], fields[2]
		if fields[0] != pgid || ppid != "1" {
			continue
		}
		if !IsTmuxServerCommand(strings.Join(fields[3:], " ")) {
			continue
		}
		servers[pid] = true
	}
	return servers
}

// IsTmuxServerCommand reports whether a process command line is a tmux
// invocation. The tmux server keeps the argv of whatever invocation founded it,
// so this matches `tmux -u -L <socket> new-session ...` as readily as a bare
// `tmux`; the daemon-shape checks in LiveTmuxServerPIDs are what make the
// combination specific to a server.
func IsTmuxServerCommand(command string) bool {
	command = strings.TrimSpace(command)
	if command == "" {
		return false
	}
	binary := command
	if idx := strings.IndexByte(binary, ' '); idx >= 0 {
		binary = binary[:idx]
	}
	return filepath.Base(binary) == "tmux"
}
