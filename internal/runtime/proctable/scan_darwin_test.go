//go:build darwin

package proctable

import (
	"testing"

	"github.com/gastownhall/gascity/internal/runtime"
)

func TestScanDarwinRecordsDoesNotMatchAliasAndWorkDirWithoutAgentCommand(t *testing.T) {
	const workDir = "/city/.gc/worktrees/qcore/crew/dalinar"
	records := map[int]psRecord{
		500: {
			pid:     500,
			ppid:    1,
			command: "claude",
			cwd:     workDir,
			env:     map[string]string{"GC_ALIAS": "qcore/dalinar"},
		},
	}

	got := scanDarwinRecords(records, runtime.ProcessTarget{
		SessionID: "hq-session",
		Alias:     "qcore/dalinar",
		WorkDir:   workDir,
	})
	if len(got) != 0 {
		t.Fatalf("scanDarwinRecords returned alias-only runtimes, want none: %+v", got)
	}
}

func TestScanDarwinRecordsExcludesInfrastructureRoot(t *testing.T) {
	records := map[int]psRecord{
		100: {
			pid:     100,
			ppid:    1,
			command: "tmux: server",
			env:     map[string]string{"GC_SESSION_ID": "hq-session"},
		},
		101: {
			pid:     101,
			ppid:    100,
			command: "claude",
			env:     map[string]string{"GC_SESSION_ID": "hq-session"},
		},
	}

	got := scanDarwinRecords(records, runtime.ProcessTarget{SessionID: "hq-session"})
	if len(got) != 1 || got[0].PID != 101 {
		t.Fatalf("scanDarwinRecords = %+v, want only agent pid 101", got)
	}
}

func TestParseDarwinCWDs(t *testing.T) {
	got := parseDarwinCWDs("p100\nfcwd\nn/city/worktree\np200\nfcwd\nn/city/other\n")
	if len(got) != 2 || got[100] != "/city/worktree" || got[200] != "/city/other" {
		t.Fatalf("parseDarwinCWDs = %#v", got)
	}
}

func TestScanDarwinRecordsMatchesAgentCommandAndWorkDirWithoutManagedEnv(t *testing.T) {
	const workDir = "/city/.gc/worktrees/qcore/crew/dalinar"
	records := map[int]psRecord{
		500: {
			pid:     500,
			ppid:    1,
			command: "bun",
			argv:    []string{"bun", "/usr/local/bin/omp", "--hook", ".omp/hooks/gc-hook.ts"},
			cwd:     workDir,
			env:     map[string]string{},
		},
	}

	got := scanDarwinRecords(records, runtime.ProcessTarget{
		SessionID:    "hq-session",
		WorkDir:      workDir,
		ProcessNames: []string{"omp"},
	})
	if len(got) != 1 {
		t.Fatalf("scanDarwinRecords returned %d runtimes, want 1: %+v", len(got), got)
	}
	if got[0].PID != 500 || got[0].SessionID != "" || got[0].ProcessName != "omp" || got[0].WorkDir != workDir {
		t.Fatalf("scanDarwinRecords runtime = %+v, want pid=500 process=omp workdir=%q without managed env", got[0], workDir)
	}
}

func TestScanDarwinRecordsDoesNotMatchDifferentManagedSessionInSameWorkDir(t *testing.T) {
	const workDir = "/shared/worktree"
	records := map[int]psRecord{
		500: {
			pid:     500,
			ppid:    1,
			command: "claude",
			argv:    []string{"claude"},
			cwd:     workDir,
			env:     map[string]string{"GC_SESSION_ID": "other-session"},
		},
	}

	got := scanDarwinRecords(records, runtime.ProcessTarget{
		SessionID:    "target-session",
		WorkDir:      workDir,
		ProcessNames: []string{"claude"},
	})
	if len(got) != 0 {
		t.Fatalf("scanDarwinRecords matched a different managed session: %+v", got)
	}
}
