package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/events"
)

func convoyPolicyTestConfig() *config.City {
	return &config.City{
		Convoys: config.ConvoyPolicyConfig{RequireOwnedTarget: true, ForbidDefaultTarget: true},
		Rigs:    []config.Rig{{Name: "rig", Prefix: "gc", DefaultBranch: "main"}},
	}
}

func TestConvoyCreateOwnedPolicyRejectsBeforeMutation(t *testing.T) {
	store := beads.NewMemStore()
	issue, err := store.Create(beads.Bead{Title: "work", Type: "task"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := convoyPolicyTestConfig()
	var stdout, stderr bytes.Buffer
	code := doConvoyCreateWithOptionsJSON(store, cfg, "/city", events.Discard, []string{"owned", issue.ID}, convoyCreateOptions{Owned: true}, false, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "owned convoy requires an explicit target") {
		t.Fatalf("code=%d stderr=%q, want target policy rejection", code, stderr.String())
	}
	convoys, err := store.List(beads.ListQuery{Type: "convoy", IncludeClosed: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(convoys) != 0 {
		t.Fatalf("convoys after rejected create = %d, want 0", len(convoys))
	}
}

func TestConvoyCreateOwnedPolicyRejectsRigDefaultTarget(t *testing.T) {
	store := beads.NewMemStore()
	issue, err := store.Create(beads.Bead{Title: "work", Type: "task"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := convoyPolicyTestConfig()
	var stdout, stderr bytes.Buffer
	code := doConvoyCreateWithOptionsJSON(store, cfg, "/city", events.Discard, []string{"owned", issue.ID}, convoyCreateOptions{Owned: true, Fields: ConvoyFields{Target: "main"}}, false, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), `target "main" is the owning rig default branch`) {
		t.Fatalf("code=%d stderr=%q, want default-target rejection", code, stderr.String())
	}
}

func TestConvoyCreateOwnedPolicyPersistsNonDefaultTarget(t *testing.T) {
	store := beads.NewMemStore()
	issue, err := store.Create(beads.Bead{Title: "work", Type: "task"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := convoyPolicyTestConfig()
	var stdout, stderr bytes.Buffer
	code := doConvoyCreateWithOptionsJSON(store, cfg, "/city", events.Discard, []string{"owned", issue.ID}, convoyCreateOptions{Owned: true, Fields: ConvoyFields{Target: "feature/work"}}, false, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%q", code, stderr.String())
	}
	convoys, err := store.List(beads.ListQuery{Type: "convoy", IncludeClosed: true})
	if err != nil || len(convoys) != 1 {
		t.Fatalf("convoys=%v err=%v, want one", convoys, err)
	}
	if got := convoys[0].Metadata["target"]; got != "feature/work" {
		t.Fatalf("target=%q, want feature/work", got)
	}
}

func TestConvoyTargetOwnedPolicyRejectsRigDefault(t *testing.T) {
	store := beads.NewMemStore()
	convoy, err := store.Create(beads.Bead{Title: "owned", Type: "convoy", Labels: []string{"owned"}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := convoyPolicyTestConfig()
	var stdout, stderr bytes.Buffer
	code := doConvoyTargetJSONWithConfig(store, cfg, "/city", []string{convoy.ID, "main"}, false, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), `target "main" is the owning rig default branch`) {
		t.Fatalf("code=%d stderr=%q, want default-target rejection", code, stderr.String())
	}
	got, _ := store.Get(convoy.ID)
	if got.Metadata["target"] != "" {
		t.Fatalf("target metadata=%q after rejected mutation, want empty", got.Metadata["target"])
	}
}

func TestConvoyLandLegacyOwnedPolicyRejectsBeforeClose(t *testing.T) {
	store := beads.NewMemStore()
	convoy, err := store.Create(beads.Bead{Title: "legacy", Type: "convoy", Labels: []string{"owned"}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := convoyPolicyTestConfig()
	var stdout, stderr bytes.Buffer
	code := doConvoyLandJSONWithConfig(store, events.Discard, cfg, "/city", []string{convoy.ID}, landOpts{Force: true}, false, &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "owned convoy requires an explicit target") {
		t.Fatalf("code=%d stderr=%q, want legacy-target rejection", code, stderr.String())
	}
	got, _ := store.Get(convoy.ID)
	if got.Status == "closed" {
		t.Fatal("legacy invalid owned convoy was closed")
	}
}

func TestConvoyDefaultBranchForSlingFallsBackToMain(t *testing.T) {
	cfg := &config.City{Rigs: []config.Rig{{Name: "frontend", Path: t.TempDir(), Prefix: "FE"}}}
	got := convoyDefaultBranchForSling(cfg, t.TempDir(), "gc-1", config.Agent{Dir: "frontend"})
	if got != "main" {
		t.Fatalf("convoyDefaultBranchForSling() = %q, want main fallback", got)
	}
}
