package sling

import (
	"testing"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/formula"
)

// liveWorkflowBead reports whether a bead is usable as part of a live
// workflow: open, not marked failed, not fenced, and with nothing left under
// the gc.deferred_* keys (the fence's activation clears them one by one).
func liveWorkflowBead(b beads.Bead) bool {
	return b.Status == "open" &&
		b.Metadata[beadmeta.MoleculeFailedMetadataKey] == "" &&
		b.Metadata[beadmeta.InstantiatingMetadataKey] == "" &&
		b.Type != "gate" &&
		b.Metadata[beadmeta.DeferredTypeMetadataKey] == "" &&
		b.Metadata[beadmeta.DeferredAssigneeMetadataKey] == "" &&
		b.Metadata[beadmeta.DeferredRoutedToMetadataKey] == "" &&
		b.Metadata[beadmeta.DeferredExecutionRoutedToMetadataKey] == ""
}

// TestGraphV2SameKeyLaunchDoesNotAdoptFencedChainWithoutAbortMark: a formulas
// v2 launch that finds the sole live root under its gc.graphv2_root_key
// adopts it as the idempotent duplicate. A client killed mid-instantiation
// leaves the chain fenced — type gate, gc.instantiating=true, routing under
// gc.deferred_* — with no molecule_failed, because markFailed never ran; a
// client killed inside the activation loop leaves an activated root over
// fenced steps. closeFailedGraphV2Roots closes only molecule_failed roots, so
// existingGraphV2Root hands the root back as the workflow: the launch reports
// success and returns a chain whose steps nothing will ever activate.
//
// The one contract asserted: a fenced, never-failed chain is not adopted as
// the live workflow. A refusal (an error from either stage) or a non-adoption
// (nil) passes; adopting the original chain passes only if the root and its
// step carry exactly the values a healthy activation restores; adopting a
// fresh replacement passes if that chain is live. Retiring the abandoned
// chain is a separate contract and is not asserted here; the fixture carries
// no age or ownership signal.
func TestGraphV2SameKeyLaunchDoesNotAdoptFencedChainWithoutAbortMark(t *testing.T) {
	cases := []struct {
		name       string
		rootFenced bool
	}{
		{name: "root and step fenced", rootFenced: true},
		{name: "root activated, step fenced", rootFenced: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := beads.NewMemStore()
			const key = "wf|convoy-1"
			rootMeta := map[string]string{
				beadmeta.KindMetadataKey:            beadmeta.KindWorkflow,
				beadmeta.FormulaContractMetadataKey: beadmeta.FormulaContractGraphV2,
				beadmeta.Graphv2RootKeyMetadataKey:  key,
			}
			rootType := "task"
			if tc.rootFenced {
				rootType = "gate"
				rootMeta[beadmeta.InstantiatingMetadataKey] = "true"
				rootMeta[beadmeta.DeferredTypeMetadataKey] = "task"
				rootMeta[beadmeta.DeferredRoutedToMetadataKey] = "gascity/control-dispatcher"
			} else {
				rootMeta[beadmeta.RoutedToMetadataKey] = "gascity/control-dispatcher"
			}
			root, err := store.Create(beads.Bead{Title: "workflow", Type: rootType, Metadata: rootMeta})
			if err != nil {
				t.Fatalf("create root: %v", err)
			}
			step, err := store.Create(beads.Bead{
				Title: "work",
				Type:  "gate",
				Metadata: map[string]string{
					beadmeta.RootBeadIDMetadataKey:       root.ID,
					beadmeta.StepRefMetadataKey:          "wf.work",
					beadmeta.InstantiatingMetadataKey:    "true",
					beadmeta.DeferredTypeMetadataKey:     "task",
					beadmeta.DeferredRoutedToMetadataKey: "gascity/worker",
				},
			})
			if err != nil {
				t.Fatalf("create fenced step: %v", err)
			}

			recipe := &formula.Recipe{
				Name: "wf",
				Steps: []formula.RecipeStep{{
					ID:     "wf",
					Title:  "workflow",
					Type:   "task",
					IsRoot: true,
					Metadata: map[string]string{
						beadmeta.KindMetadataKey:            beadmeta.KindWorkflow,
						beadmeta.FormulaContractMetadataKey: beadmeta.FormulaContractGraphV2,
						beadmeta.Graphv2RootKeyMetadataKey:  key,
					},
				}},
			}

			if err := closeFailedGraphV2Roots(store, recipe); err != nil {
				return // refused before the lookup: not an adoption
			}
			existing, err := existingGraphV2Root(store, recipe)
			if err != nil {
				return // refused: not an adoption
			}
			if existing == nil {
				return // not adopted
			}

			describe := func(b beads.Bead) string {
				return "status=" + b.Status + " type=" + b.Type + " assignee=" + b.Assignee +
					" gc.routed_to=" + b.Metadata[beadmeta.RoutedToMetadataKey] +
					" gc.instantiating=" + b.Metadata[beadmeta.InstantiatingMetadataKey] +
					" molecule_failed=" + b.Metadata[beadmeta.MoleculeFailedMetadataKey] +
					" gc.deferred_type=" + b.Metadata[beadmeta.DeferredTypeMetadataKey]
			}
			fail := func(b beads.Bead) {
				t.Helper()
				t.Fatalf("same-key launch adopted root %s as the live workflow while bead %s of that chain is not live (%s): a client killed mid-instantiation leaves exactly this shape, closeFailedGraphV2Roots closes only molecule_failed roots, and nothing else on the launch path refuses, closes or resumes it — the launch reports success and returns a workflow whose fenced steps nothing will ever activate",
					existing.RootID, b.ID, describe(b))
			}
			if existing.RootID == root.ID {
				// The original chain was adopted: it must carry exactly what a
				// healthy activation restores.
				rootNow, err := store.Get(root.ID)
				if err != nil {
					t.Fatalf("Get(root): %v", err)
				}
				if !liveWorkflowBead(rootNow) || rootNow.Type != "task" || rootNow.Metadata[beadmeta.RoutedToMetadataKey] != "gascity/control-dispatcher" {
					fail(rootNow)
				}
				stepNow, err := store.Get(step.ID)
				if err != nil {
					t.Fatalf("Get(step): %v", err)
				}
				if !liveWorkflowBead(stepNow) || stepNow.Type != "task" || stepNow.Metadata[beadmeta.RoutedToMetadataKey] != "gascity/worker" {
					fail(stepNow)
				}
				return
			}
			// A fresh replacement was adopted: its chain must be live.
			adopted, err := store.Get(existing.RootID)
			if err != nil {
				t.Fatalf("Get(%s): %v", existing.RootID, err)
			}
			members, err := store.ListByMetadata(map[string]string{beadmeta.RootBeadIDMetadataKey: existing.RootID}, 0, beads.IncludeClosed)
			if err != nil {
				t.Fatalf("list members of %s: %v", existing.RootID, err)
			}
			for _, b := range append([]beads.Bead{adopted}, members...) {
				if !liveWorkflowBead(b) {
					fail(b)
				}
			}
		})
	}
}
