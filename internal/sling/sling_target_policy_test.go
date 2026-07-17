package sling

import (
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	convoycore "github.com/gastownhall/gascity/internal/convoy"
	"github.com/gastownhall/gascity/internal/runtime"
)

func TestDoSlingOwnedRequiresConvoyTargetBeforeRouting(t *testing.T) {
	runner := newFakeRunner()
	store := seededStore("gc-1")
	cfg := &config.City{
		Convoys: config.ConvoyPolicyConfig{RequireOwnedTarget: true, ForbidDefaultTarget: true},
		Agents:  []config.Agent{{Name: "worker"}},
	}
	deps := testDeps(cfg, runtime.NewFake(), runner.run)
	deps.Store = store
	_, err := DoSling(SlingOpts{Target: cfg.Agents[0], BeadOrFormula: "gc-1", Owned: true}, deps, store)
	if err == nil || !strings.Contains(err.Error(), "owned convoy requires an explicit target") {
		t.Fatalf("DoSling() error = %v, want owned target error", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("routing calls = %d, want 0 before policy validation", len(runner.calls))
	}
}

func TestDoSlingOwnedPersistsNonDefaultConvoyTarget(t *testing.T) {
	runner := newFakeRunner()
	store := seededStore("gc-1")
	cfg := &config.City{
		Convoys: config.ConvoyPolicyConfig{RequireOwnedTarget: true, ForbidDefaultTarget: true},
		Rigs:    []config.Rig{{Name: "rig", Prefix: "gc", Path: "/rig", DefaultBranch: "main"}},
		Agents:  []config.Agent{{Name: "worker", Dir: "rig"}},
	}
	deps := testDeps(cfg, runtime.NewFake(), runner.run)
	deps.Store = store
	result, err := DoSling(SlingOpts{Target: cfg.Agents[0], BeadOrFormula: "gc-1", Owned: true, ConvoyTarget: "feature/work"}, deps, store)
	if err != nil {
		t.Fatalf("DoSling() error = %v", err)
	}
	convoy, err := store.Get(result.ConvoyID)
	if err != nil {
		t.Fatalf("get auto-convoy: %v", err)
	}
	if got := convoy.Metadata["target"]; got != "feature/work" {
		t.Fatalf("auto-convoy target = %q, want feature/work", got)
	}
}

func TestDoSlingBatchOwnedRequiresConvoyTargetBeforeRouting(t *testing.T) {
	runner := newFakeRunner()
	store := beads.NewMemStore()
	convoy, err := store.Create(beads.Bead{Title: "owned convoy", Type: "convoy", Labels: []string{"owned"}})
	if err != nil {
		t.Fatal(err)
	}
	child, err := store.Create(beads.Bead{Title: "work", Type: "task", Status: "open"})
	if err != nil {
		t.Fatal(err)
	}
	if err := convoycore.TrackItem(store, convoy.ID, child.ID); err != nil {
		t.Fatal(err)
	}
	cfg := &config.City{Convoys: config.ConvoyPolicyConfig{RequireOwnedTarget: true}, Agents: []config.Agent{{Name: "worker"}}}
	deps := testDeps(cfg, runtime.NewFake(), runner.run)
	deps.Store = store
	_, err = DoSlingBatch(SlingOpts{Target: cfg.Agents[0], BeadOrFormula: convoy.ID, Owned: true}, deps, store)
	if err == nil || !strings.Contains(err.Error(), "owned convoy requires an explicit target") {
		t.Fatalf("DoSlingBatch() error = %v, want owned target error", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("routing calls = %d, want 0 before policy validation", len(runner.calls))
	}
}

func TestDoSlingCitySourceUsesRigAgentDefaultBranch(t *testing.T) {
	runner := newFakeRunner()
	store := seededStore("gc-1")
	cfg := &config.City{
		Convoys: config.ConvoyPolicyConfig{RequireOwnedTarget: true, ForbidDefaultTarget: true},
		Rigs:    []config.Rig{{Name: "frontend", Prefix: "FE", Path: "/frontend", DefaultBranch: "develop"}},
		Agents:  []config.Agent{{Name: "worker", Dir: "frontend"}},
	}
	deps := testDeps(cfg, runtime.NewFake(), runner.run)
	deps.Store = store
	_, err := DoSling(SlingOpts{Target: cfg.Agents[0], BeadOrFormula: "gc-1", Owned: true, ConvoyTarget: "develop"}, deps, store)
	if err == nil || !strings.Contains(err.Error(), `target "develop" is the owning rig default branch`) {
		t.Fatalf("DoSling() error = %v, want rig-default rejection", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("routing calls = %d, want 0", len(runner.calls))
	}
}
