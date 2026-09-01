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

// The infrastructure match is exact on the path-stripped name: the tmux
// executable as ps reports it on Darwin, and the titles tmux sets through
// setproctitle as Linux comm reports them (newline-terminated). Anything
// else — above all a tmux-* wrapper — is a candidate agent root.
func TestIsInfrastructureCommandMatchesExactNamesOnly(t *testing.T) {
	for _, command := range []string{"tmux", "/opt/homebrew/bin/tmux", "tmux: server", "tmux: client", "tmux: server\n"} {
		if !isInfrastructureCommand(command) {
			t.Errorf("isInfrastructureCommand(%q) = false, want true", command)
		}
	}
	for _, command := range []string{"", "claude", "/bin/bash", "tmux-wrapper", "/usr/local/bin/tmux-wrapper", "tmuxinator", "my-tmux"} {
		if isInfrastructureCommand(command) {
			t.Errorf("isInfrastructureCommand(%q) = true, want false: only an exact tmux name is infrastructure", command)
		}
	}
}
