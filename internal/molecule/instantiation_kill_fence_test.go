package molecule

import (
	"context"
	"runtime"
	"testing"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/formula"
)

// killingStore models a store call that never returns on the current
// implementation: the client died mid-instantiation. Returning an error is
// the wrong model — Instantiate answers every error with markFailed, which
// stamps molecule_failed and clears the fence — so the wrapped call ends the
// goroutine running Instantiate instead (runtime.Goexit runs deferred calls,
// unlike a hard kill; this path has none that write), leaving whatever the
// store already holds.
type killingStore struct {
	*beads.MemStore
	killAtDepAdd     bool
	killOnActivation int // kill when the Nth fence-clearing Update arrives (0 = never)
	activations      int
	killed           bool
}

func (s *killingStore) DepAdd(issueID, dependsOnID, depType string) error {
	if s.killAtDepAdd {
		s.killed = true
		runtime.Goexit()
	}
	return s.MemStore.DepAdd(issueID, dependsOnID, depType)
}

func (s *killingStore) Update(id string, opts beads.UpdateOpts) error {
	if _, activating := opts.Metadata[InstantiatingMetadataKey]; activating && s.killOnActivation > 0 {
		s.activations++
		if s.activations == s.killOnActivation {
			s.killed = true
			runtime.Goexit()
		}
	}
	return s.MemStore.Update(id, opts)
}

func killFenceRecipe() *formula.Recipe {
	return &formula.Recipe{
		Name: "wf",
		Steps: []formula.RecipeStep{
			{
				ID:       "wf",
				Title:    "Workflow",
				Type:     "task",
				IsRoot:   true,
				Assignee: "controller",
				Metadata: map[string]string{
					"gc.kind":      "workflow",
					"gc.routed_to": "gascity/control-dispatcher",
				},
			},
			{
				ID:       "wf.body",
				Title:    "Body",
				Type:     "task",
				Assignee: "worker",
				Metadata: map[string]string{
					"gc.kind":      "scope",
					"gc.routed_to": "gascity/worker",
				},
			},
			{
				ID:    "wf.workflow-finalize",
				Title: "Finalize",
				Type:  "task",
				Metadata: map[string]string{
					"gc.kind":                  "workflow-finalize",
					"gc.execution_routed_to":   "gascity/control-dispatcher",
					"gc.step_timeout":          "5m",
					"gc.control_dispatch_kind": "finalize",
				},
			},
		},
		Deps: []formula.RecipeDep{
			{StepID: "wf.body", DependsOnID: "wf", Type: "parent-child"},
			{StepID: "wf.workflow-finalize", DependsOnID: "wf", Type: "parent-child"},
			{StepID: "wf.workflow-finalize", DependsOnID: "wf.body", Type: "blocks"},
			{StepID: "wf", DependsOnID: "wf.workflow-finalize", Type: "blocks"},
		},
	}
}

// TestInstantiateKilledMidFenceLeavesBeadsFencedWithoutAbortMark pins the
// store state a client that dies mid-instantiation leaves behind on the
// sequential (fenced) graph workflow path. Whether it dies before the
// explicit dependency wiring completes, at the first activation, or after
// the root alone has been activated, every bead that was not activated is
// still fenced — type gate, assignee and routing held under gc.deferred_*,
// gc.instantiating=true — and no bead carries molecule_failed, the only mark
// the same-key recovery paths (sling, drain replay, cook) look for.
func TestInstantiateKilledMidFenceLeavesBeadsFencedWithoutAbortMark(t *testing.T) {
	prev := IsGraphApplyEnabled()
	SetGraphApplyEnabled(false)
	t.Cleanup(func() { SetGraphApplyEnabled(prev) })

	cases := []struct {
		name          string
		store         func(*beads.MemStore) *killingStore
		rootActivated bool // the root was un-fenced before the kill
		rootWired     bool // the root's blocks edge on finalize was written before the kill
	}{
		{
			name:  "before explicit dependency wiring completes",
			store: func(m *beads.MemStore) *killingStore { return &killingStore{MemStore: m, killAtDepAdd: true} },
		},
		{
			name:      "at the first activation",
			store:     func(m *beads.MemStore) *killingStore { return &killingStore{MemStore: m, killOnActivation: 1} },
			rootWired: true,
		},
		{
			name:          "after the root's activation",
			store:         func(m *beads.MemStore) *killingStore { return &killingStore{MemStore: m, killOnActivation: 2} },
			rootActivated: true,
			rootWired:     true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := beads.NewMemStore()
			store := tc.store(base)
			returned := make(chan error, 1)
			done := make(chan struct{})
			go func() {
				defer close(done)
				_, err := Instantiate(context.Background(), store, killFenceRecipe(), Options{})
				returned <- err
			}()
			<-done
			select {
			case err := <-returned:
				t.Fatalf("Instantiate returned (err=%v); the store never modeled a dead client", err)
			default:
			}
			if !store.killed {
				t.Fatal("the kill point was never reached")
			}

			all, err := base.ListOpen()
			if err != nil {
				t.Fatalf("ListOpen: %v", err)
			}
			if len(all) != 3 {
				t.Fatalf("open beads = %d, want the root and both steps", len(all))
			}
			byTitle := make(map[string]beads.Bead, len(all))
			for _, b := range all {
				full, err := base.Get(b.ID)
				if err != nil {
					t.Fatalf("Get(%s): %v", b.ID, err)
				}
				byTitle[full.Title] = full
				if got := full.Metadata[beadmeta.MoleculeFailedMetadataKey]; got != "" {
					t.Errorf("bead %s molecule_failed = %q, want unset: only markFailed writes it and a dead client never runs markFailed", full.ID, got)
				}
			}
			root, body, finalize := byTitle["Workflow"], byTitle["Body"], byTitle["Finalize"]
			if root.ID == "" || body.ID == "" || finalize.ID == "" {
				t.Fatalf("beads by title = %v, want Workflow, Body and Finalize", byTitle)
			}

			// A fenced bead keeps the exact values a healthy activation would
			// restore, and exposes none of them.
			wantFenced := func(b beads.Bead, wantType, wantAssignee, wantRouted, wantExecRouted string) {
				t.Helper()
				if got := b.Metadata[InstantiatingMetadataKey]; got != "true" {
					t.Errorf("bead %s gc.instantiating = %q, want true: the fence outlives the client", b.ID, got)
				}
				if b.Type != "gate" || b.Assignee != "" {
					t.Errorf("bead %s type=%q assignee=%q, want gate with the assignee deferred", b.ID, b.Type, b.Assignee)
				}
				if b.Metadata[beadmeta.RoutedToMetadataKey] != "" || b.Metadata[beadmeta.ExecutionRoutedToMetadataKey] != "" {
					t.Errorf("bead %s exposes live routing while fenced: %#v", b.ID, b.Metadata)
				}
				if got := b.Metadata[beadmeta.DeferredTypeMetadataKey]; got != wantType {
					t.Errorf("bead %s gc.deferred_type = %q, want %q", b.ID, got, wantType)
				}
				if got := b.Metadata[beadmeta.DeferredAssigneeMetadataKey]; got != wantAssignee {
					t.Errorf("bead %s gc.deferred_assignee = %q, want %q", b.ID, got, wantAssignee)
				}
				if got := b.Metadata[beadmeta.DeferredRoutedToMetadataKey]; got != wantRouted {
					t.Errorf("bead %s gc.deferred_routed_to = %q, want %q", b.ID, got, wantRouted)
				}
				if got := b.Metadata[beadmeta.DeferredExecutionRoutedToMetadataKey]; got != wantExecRouted {
					t.Errorf("bead %s gc.deferred_execution_routed_to = %q, want %q", b.ID, got, wantExecRouted)
				}
			}
			wantFenced(body, "task", "worker", "gascity/worker", "")
			wantFenced(finalize, "task", "", "", "gascity/control-dispatcher")
			if tc.rootActivated {
				if root.Metadata[InstantiatingMetadataKey] != "" || root.Type != "task" || root.Assignee != "controller" || root.Metadata[beadmeta.RoutedToMetadataKey] != "gascity/control-dispatcher" {
					t.Errorf("root %s should be the one activated bead: type=%q assignee=%q instantiating=%q metadata=%#v", root.ID, root.Type, root.Assignee, root.Metadata[InstantiatingMetadataKey], root.Metadata)
				}
			} else {
				wantFenced(root, "task", "controller", "gascity/control-dispatcher", "")
			}

			// The root's blocks edge on finalize cannot be embedded at Create
			// (finalize does not exist yet), so it tells the wiring phase apart.
			deps, err := base.DepList(root.ID, "down")
			if err != nil {
				t.Fatalf("DepList(root): %v", err)
			}
			rootWired := false
			for _, d := range deps {
				if d.DependsOnID == finalize.ID && d.Type == "blocks" {
					rootWired = true
				}
			}
			if rootWired != tc.rootWired {
				t.Errorf("root -> finalize blocks edge present = %v, want %v (deps=%v)", rootWired, tc.rootWired, deps)
			}
		})
	}
}
