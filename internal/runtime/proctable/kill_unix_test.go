//go:build linux || darwin

package proctable

import (
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestKillByPIDRefusesLowPIDs(t *testing.T) {
	for _, pid := range []int{-1, 0, 1} {
		if err := KillByPID(pid); err == nil {
			t.Errorf("KillByPID(%d) succeeded, want error", pid)
		}
	}
}

func TestKillByPIDAlreadyGoneIsSuccess(t *testing.T) {
	// Spawn a short-lived process and wait for it to exit, then try to kill it.
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatalf("spawning test process: %v", err)
	}
	pid := cmd.ProcessState.Pid()
	// Process is already gone. KillByPID should return nil (ESRCH → success).
	if err := KillByPID(pid); err != nil {
		t.Fatalf("KillByPID(%d) for already-dead process: %v", pid, err)
	}
}

func TestSignalPIDGroupThenFallback(t *testing.T) {
	var got []int
	err := signalPIDWith(12345, syscall.SIGTERM, func(pid int, sig syscall.Signal) error {
		if sig != syscall.SIGTERM {
			t.Fatalf("signal = %v, want SIGTERM", sig)
		}
		got = append(got, pid)
		if pid < 0 {
			return syscall.ESRCH
		}
		return nil
	})
	if err != nil {
		t.Fatalf("signalPIDWith(): %v", err)
	}
	want := []int{-12345, 12345}
	if len(got) != len(want) {
		t.Fatalf("signal calls = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("signal calls = %v, want %v", got, want)
		}
	}
}

func TestSignalPIDGroupSuccessSkipsFallback(t *testing.T) {
	var got []int
	err := signalPIDWith(12345, syscall.SIGTERM, func(pid int, sig syscall.Signal) error {
		if sig != syscall.SIGTERM {
			t.Fatalf("signal = %v, want SIGTERM", sig)
		}
		got = append(got, pid)
		return nil
	})
	if err != nil {
		t.Fatalf("signalPIDWith(): %v", err)
	}
	want := []int{-12345}
	if !slices.Equal(got, want) {
		t.Fatalf("signal calls = %v, want %v", got, want)
	}
}

// TestKillByPIDConfirmedDeadBeforeReturn drives the injected core: a process
// still runnable after SIGKILL (e.g. wedged in D-state) must yield an error so
// a caller can refuse to start a racing replacement, while one that becomes
// dead (gone or zombie) after SIGKILL returns nil.
func TestKillByPIDConfirmedDeadBeforeReturn(t *testing.T) {
	t.Run("survives SIGKILL -> error", func(t *testing.T) {
		var signals []syscall.Signal
		kill := func(_ int, sig syscall.Signal) error {
			// Record every delivery attempt. signalPIDWith signals the process
			// group (negative pid) first and returns on success, so with this
			// always-succeeding fake these are the group deliveries; the
			// assertion below only checks the final escalation is SIGKILL.
			signals = append(signals, sig)
			return nil
		}
		termLive := func(int) bool { return true } // never exits on SIGTERM
		runLive := func(int) bool { return true }  // survives SIGKILL too
		err := killByPID(4321, kill, termLive, runLive, 5*time.Millisecond, 5*time.Millisecond)
		if err == nil {
			t.Fatal("killByPID returned nil for a process that survived SIGKILL")
		}
		if !strings.Contains(err.Error(), "not confirmed dead") {
			t.Fatalf("error = %v, want 'not confirmed dead'", err)
		}
		if len(signals) == 0 || signals[len(signals)-1] != syscall.SIGKILL {
			t.Fatalf("signals = %v, want SIGKILL escalation", signals)
		}
	})

	t.Run("dies after SIGKILL -> nil", func(t *testing.T) {
		kill := func(int, syscall.Signal) error { return nil }
		termLive := func(int) bool { return true } // ignores SIGTERM
		var kills int
		runLive := func(int) bool {
			kills++
			return kills <= 1 // alive on first confirm poll, dead after
		}
		if err := killByPID(4321, kill, termLive, runLive, 5*time.Millisecond, time.Second); err != nil {
			t.Fatalf("killByPID: %v", err)
		}
	})

	t.Run("exits during SIGTERM grace -> no SIGKILL", func(t *testing.T) {
		var sawKill bool
		kill := func(_ int, sig syscall.Signal) error {
			if sig == syscall.SIGKILL {
				sawKill = true
			}
			return nil
		}
		var polls int
		termLive := func(int) bool {
			polls++
			return polls <= 1 // alive at entry, exits before grace elapses
		}
		runLive := func(int) bool { return false }
		if err := killByPID(4321, kill, termLive, runLive, time.Second, time.Second); err != nil {
			t.Fatalf("killByPID: %v", err)
		}
		if sawKill {
			t.Fatal("SIGKILL sent even though the process exited during grace")
		}
	})
}

func TestWaitUntilRespectsZeroTimeout(t *testing.T) {
	if !waitUntil(func() bool { return true }, 0) {
		t.Fatal("waitUntil should observe an already-satisfied condition at zero timeout")
	}
	if waitUntil(func() bool { return false }, 0) {
		t.Fatal("waitUntil should report false when the condition never holds at zero timeout")
	}
}

// TestKillByPIDNeverSignalsTheTmuxServer proves the hq-nie0 guard is WIRED INTO
// KillByPID, not merely implemented somewhere.
//
// This test exists because the first fix for this outage (ga-03ixvj) guarded
// terminateProcesses in the tmux package and tested THAT wiring — while the
// path the city actually died through was
// tmux.Provider.TerminateRuntime -> proctable.KillByPID, which has no relation
// to terminateProcesses. A guard proven wired into one kill family says nothing
// about the other. So: drive the real exported KillByPID with a stubbed server
// lookup and a captured kill syscall, and assert that no signal of any kind
// reaches the server PID.
//
// The stand-in server is a real LIVE process. That is the point: killByPID
// returns early for an already-dead PID, so a fabricated PID would make this
// test pass whether or not the guard exists.
func TestKillByPIDNeverSignalsTheTmuxServer(t *testing.T) {
	serverPID := startGuardedTestProcess(t)

	origServers, origKill := tmuxServerPIDsFn, killSyscallFn
	t.Cleanup(func() { tmuxServerPIDsFn, killSyscallFn = origServers, origKill })

	tmuxServerPIDsFn = func() map[int]bool { return map[int]bool{serverPID: true} }

	var signaled []string
	killSyscallFn = func(pid int, sig syscall.Signal) error {
		signaled = append(signaled, sig.String()+":"+strconv.Itoa(pid))
		return nil
	}

	if err := KillByPID(serverPID); err != nil {
		// Refusal must read as success: a tmux server is not an orphan racing
		// a replacement, and an error here would refuse every mayor restart.
		t.Fatalf("KillByPID(server) = %v, want nil (refusal is success)", err)
	}

	if len(signaled) != 0 {
		t.Fatalf("KillByPID signaled the tmux server: %v — this is the whole-city outage (hq-nie0)", signaled)
	}
}

// startGuardedTestProcess spawns a real, live process in its OWN process group
// and returns its PID. A live target is required because killByPID returns
// early when the PID is already gone, which would make a guard test pass
// vacuously. Its own process group makes the kill(-pid) that signalPIDWith
// tries first land on exactly this process and nothing else.
func startGuardedTestProcess(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting test process: %v", err)
	}
	// Reap continuously. Without this the killed child lingers as a zombie,
	// kill(pid, 0) keeps reporting it live, and KillByPID burns its full
	// SIGTERM grace before escalating — turning each guard test into a
	// multi-second wait for no added coverage.
	go func() { _ = cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	return cmd.Process.Pid
}

// TestKillByPIDSignalsTheProcessGroup pins WHY sparing the server matters, and
// pins that ordinary targets are still killed normally. signalPIDWith tries
// kill(-pid) first — the process GROUP — and a tmux server is its own
// process-group leader (it setsid()s when it daemonizes), so an unguarded kill
// of the server PID takes out the server's whole group.
func TestKillByPIDSignalsTheProcessGroup(t *testing.T) {
	agentPID := startGuardedTestProcess(t)

	origServers, origKill := tmuxServerPIDsFn, killSyscallFn
	t.Cleanup(func() { tmuxServerPIDsFn, killSyscallFn = origServers, origKill })

	// A server set that does NOT contain the target: the guard must not fire.
	tmuxServerPIDsFn = func() map[int]bool { return map[int]bool{12611: true} }

	var signaled []int
	killSyscallFn = func(pid int, sig syscall.Signal) error {
		signaled = append(signaled, pid)
		return syscall.Kill(pid, sig)
	}

	if err := KillByPID(agentPID); err != nil {
		t.Fatalf("KillByPID(live non-server) = %v, want nil", err)
	}
	if len(signaled) == 0 {
		t.Fatal("guard blocked a non-server PID — ordinary orphans must still be killed")
	}
	if signaled[0] != -agentPID {
		t.Fatalf("first signal went to %d, want %d (the process GROUP)", signaled[0], -agentPID)
	}
}

// TestKillByPIDNoKnownServersIsAPassthrough pins that the guard cannot block an
// ordinary kill. If ps fails, LiveTmuxServerPIDs returns nil and the kill must
// proceed — degrading to the previous behavior beats refusing to clean up.
func TestKillByPIDNoKnownServersIsAPassthrough(t *testing.T) {
	pid := startGuardedTestProcess(t)

	origServers, origKill := tmuxServerPIDsFn, killSyscallFn
	t.Cleanup(func() { tmuxServerPIDsFn, killSyscallFn = origServers, origKill })

	tmuxServerPIDsFn = func() map[int]bool { return nil }

	var signaled int
	killSyscallFn = func(p int, sig syscall.Signal) error {
		signaled++
		return syscall.Kill(p, sig)
	}

	if err := KillByPID(pid); err != nil {
		t.Fatalf("KillByPID with no known servers = %v, want nil", err)
	}
	if signaled == 0 {
		t.Fatal("a nil server set blocked the kill; it must degrade to unguarded behavior")
	}
}

func TestIsTmuxServerCommand(t *testing.T) {
	for _, tc := range []struct {
		command string
		want    bool
	}{
		{"tmux -u -L qlandia new-session -d -s mayor -c /city -e GC_SESSION_ID=hq-wisp-hbl68qx", true},
		{"/opt/homebrew/bin/tmux -u -L qlandia new-session -d -s mayor", true},
		{"tmux", true},
		{"claude --model opus[1m]", false},
		{"node /usr/local/bin/tmuxinator", false},
		{"", false},
	} {
		if got := IsTmuxServerCommand(tc.command); got != tc.want {
			t.Errorf("IsTmuxServerCommand(%q) = %v, want %v", tc.command, got, tc.want)
		}
	}
}
