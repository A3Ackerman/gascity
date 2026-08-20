//go:build linux || darwin

package proctable

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/gastownhall/gascity/internal/pidutil"
	"github.com/gastownhall/gascity/internal/runtime"
)

// tmuxServerPIDsFn and killSyscallFn are indirected so a test can prove the
// tmux-server guard below is actually WIRED INTO KillByPID, not merely that the
// policy function works in isolation. A guard that is correct but unreferenced
// is precisely the failure mode that let hq-nie0 recur: ga-03ixvj's guard was
// real, tested, and installed on a DIFFERENT kill path than the one that fired.
var (
	tmuxServerPIDsFn = LiveTmuxServerPIDs
	killSyscallFn    = syscall.Kill
)

// KillByPID terminates pid with SIGTERM, then SIGKILL after
// runtime.ManagedProcessStopGrace, then waits (bounded by
// runtime.ManagedProcessReapGrace) for the process to be confirmed dead — gone
// or a zombie — before returning. Already-gone processes are success. A process
// that survives its own SIGKILL past the reap grace (e.g. wedged in D-state
// under I/O) yields an error so callers can refuse to start a name-reused
// replacement that would race it for the same work.
func KillByPID(pid int) error {
	// NEVER signal a tmux server. One socket = one server = every session in
	// the city, so killing it is a whole-city outage (hq-nie0 / ga-03ixvj).
	//
	// The scan-side exclusion in scan_darwin.go / scan_linux.go is the class
	// fix and should mean the server never reaches this function at all. This
	// is the second layer, and it is here specifically because the first
	// attempt at this bug guarded terminateProcesses in the tmux package --
	// described in its own comment as "the single choke point every kill path
	// funnels through", which is not true. terminateProcesses has eight call
	// sites, all inside internal/runtime/tmux/tmux.go. KillByPID is a separate
	// kill family reached from tmux.Provider.TerminateRuntime and from
	// subprocess, and it is the one the outage came through. A guard on one
	// family says nothing about the other.
	//
	// Refusing returns SUCCESS, not an error. A tmux server is not an agent
	// process and cannot race a replacement for the same work, so there is no
	// orphan here to fail a Start over -- and returning an error would wedge
	// every mayor restart forever, trading an outage for a different outage.
	//
	// Every refusal prints. This should fire approximately never, so if it
	// does, whoever reads that log is watching a live kill path that still
	// surfaces the server as a target and needs to know which PID was spared.
	if pid > 1 && tmuxServerPIDsFn()[pid] {
		fmt.Fprintf(os.Stderr,
			"proctable: REFUSING to kill PID %d — it is a tmux server, not a session process. "+
				"Killing it would take down every session on that socket (hq-nie0).\n", pid)
		return nil
	}
	// Capture the target's start-time identity BEFORE signaling. During the
	// post-SIGKILL reap wait the PID can be reaped and recycled to an unrelated
	// process; without this, a recycled PID reads as "still alive" and we would
	// wrongly report a target that is actually gone as not-confirmed-dead,
	// spuriously refusing a legitimate Start. StartTime is empty on hosts
	// without /proc (darwin) or when the record is unreadable, in which case
	// runLive falls back to plain liveness — current behavior preserved.
	startTime, _ := pidutil.StartTime(pid)
	return killByPID(
		pid,
		killSyscallFn,
		pidAlive,
		func(p int) bool { return pidutil.AliveWithStartTime(p, startTime) },
		runtime.ManagedProcessStopGrace,
		runtime.ManagedProcessReapGrace,
	)
}

// killByPID is the signal/confirm core with its syscalls injected so the
// confirmed-dead-before-return contract can be unit-tested without real
// processes. termLive is the cheap kill(0) liveness used during the SIGTERM
// grace window (a zombie still counts as live here, matching prior behavior).
// runLive reports whether the process is still runnable — false once it is gone
// or a zombie, since a zombie can no longer execute and therefore cannot race a
// replacement.
func killByPID(
	pid int,
	kill func(int, syscall.Signal) error,
	termLive func(int) bool,
	runLive func(int) bool,
	grace, reapGrace time.Duration,
) error {
	if pid <= 1 {
		return fmt.Errorf("proctable: refusing to kill PID %d", pid)
	}
	if !termLive(pid) {
		return nil
	}
	if err := signalPIDWith(pid, syscall.SIGTERM, kill); err != nil {
		return fmt.Errorf("signal PID %d with SIGTERM: %w", pid, err)
	}
	if waitUntil(func() bool { return !termLive(pid) }, grace) {
		return nil
	}
	if err := signalPIDWith(pid, syscall.SIGKILL, kill); err != nil {
		return fmt.Errorf("signal PID %d with SIGKILL: %w", pid, err)
	}
	if waitUntil(func() bool { return !runLive(pid) }, reapGrace) {
		return nil
	}
	return fmt.Errorf("proctable: PID %d still runnable %s after SIGKILL (not confirmed dead)", pid, reapGrace)
}

// waitUntil polls done at 25ms until it reports true or timeout elapses,
// returning done's final result. Checked once up front so a zero timeout still
// observes an already-satisfied condition.
func waitUntil(done func() bool, timeout time.Duration) bool {
	if done() {
		return true
	}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			return done()
		case <-ticker.C:
			if done() {
				return true
			}
		}
	}
}

func signalPIDWith(pid int, sig syscall.Signal, kill func(int, syscall.Signal) error) error {
	if err := kill(-pid, sig); err == nil {
		return nil
	}
	err := kill(pid, sig)
	if errors.Is(err, syscall.ESRCH) {
		return nil
	}
	return err
}

func pidAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
