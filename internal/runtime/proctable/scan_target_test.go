package proctable

import (
	"errors"
	"testing"

	"github.com/gastownhall/gascity/internal/runtime"
)

func TestIdentityObservationErrorPreservesCauseAndSentinel(t *testing.T) {
	cause := errors.New("process table unavailable")
	err := identityObservationError("reading process table", cause)
	if !errors.Is(err, runtime.ErrProcessIdentityIncomplete) {
		t.Fatalf("error = %v, want ErrProcessIdentityIncomplete", err)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want original cause", err)
	}
}

func TestCitylessExactSessionNeedsProcessLocationObservation(t *testing.T) {
	target := runtime.ProcessTarget{
		SessionID:    "hq-session",
		WorkDir:      "/city/worktree",
		ProcessNames: []string{"claude"},
	}
	env := map[string]string{"GC_SESSION_ID": "hq-session"}
	if !processNeedsArgv(env, target) {
		t.Fatal("cityless exact session did not request argv observation")
	}
	if !processNeedsWorkDir(env, []string{"claude"}, target) {
		t.Fatal("cityless exact session did not request workdir observation")
	}
}
