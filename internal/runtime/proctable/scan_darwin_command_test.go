package proctable

import "testing"

func TestDarwinPSCommandIgnoresInlineTmuxEnv(t *testing.T) {
	fields := []string{
		"123",
		"45",
		"/bin/bash",
		"GC_SESSION_ID=ga-123",
		"TMUX=/private/tmp/tmux-501/default,1,0",
	}
	if got := darwinPSCommand(fields); got != "/bin/bash" {
		t.Fatalf("darwinPSCommand() = %q, want executable token only", got)
	}
	if isInfrastructureCommand(darwinPSCommand(fields)) {
		t.Fatal("regular shell with TMUX env was classified as infrastructure")
	}
}

func TestDarwinPSCommandStillIdentifiesTmuxExecutable(t *testing.T) {
	fields := []string{"123", "1", "tmux: server", "GC_SESSION_ID=ga-123"}
	if !isInfrastructureCommand(darwinPSCommand(fields)) {
		t.Fatal("tmux executable was not classified as infrastructure")
	}
}

func TestDarwinPSArgvExcludesInlineEnvironment(t *testing.T) {
	fields := []string{"123", "1", "bun", "/usr/local/bin/omp", "--hook", "GC_ALIAS=qcore/dalinar"}
	got := darwinPSArgv(fields)
	want := []string{"bun", "/usr/local/bin/omp", "--hook"}
	if len(got) != len(want) {
		t.Fatalf("darwinPSArgv() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("darwinPSArgv() = %q, want %q", got, want)
		}
	}
}
