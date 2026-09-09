package dispatch

import (
	"testing"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/formulatest"
)

// fenceLikeKilledInstantiation rewrites a materialized workflow into the shape
// a client killed during instantiation leaves behind (the sequential path's
// fenceGraphWorkflowBead): every step keeps gc.instantiating=true, its type is
// gate with the real type deferred, its routing sits under the gc.deferred_*
// keys, and molecule_failed is absent because markFailed never ran. With
// fenceRoot the root is fenced too (killed before the first activation);
// without it the root stays activated (killed inside the activation loop,
// which activates the root first).
func fenceLikeKilledInstantiation(t *testing.T, store beads.Store, rootID string, fenceRoot bool) {
	t.Helper()
	members, err := store.ListByMetadata(map[string]string{beadmeta.RootBeadIDMetadataKey: rootID}, 0)
	if err != nil {
		t.Fatalf("list workflow members of %s: %v", rootID, err)
	}
	if len(members) == 0 {
		t.Fatalf("workflow %s has no members to fence", rootID)
	}
	var ids []string
	if fenceRoot {
		ids = append(ids, rootID)
	}
	for _, member := range members {
		ids = append(ids, member.ID)
	}
	gate, empty := "gate", ""
	for _, id := range ids {
		b := mustGetBead(t, store, id)
		meta := map[string]string{
			beadmeta.InstantiatingMetadataKey: "true",
			beadmeta.DeferredTypeMetadataKey:  b.Type,
		}
		if routed := b.Metadata[beadmeta.RoutedToMetadataKey]; routed != "" {
			meta[beadmeta.DeferredRoutedToMetadataKey] = routed
			meta[beadmeta.RoutedToMetadataKey] = ""
		}
		if routed := b.Metadata[beadmeta.ExecutionRoutedToMetadataKey]; routed != "" {
			meta[beadmeta.DeferredExecutionRoutedToMetadataKey] = routed
			meta[beadmeta.ExecutionRoutedToMetadataKey] = ""
		}
		if b.Assignee != "" {
			meta[beadmeta.DeferredAssigneeMetadataKey] = b.Assignee
		}
		if err := store.Update(id, beads.UpdateOpts{Type: &gate, Assignee: &empty, Metadata: meta}); err != nil {
			t.Fatalf("fence %s: %v", id, err)
		}
	}
}

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

// TestProcessDrainReplayDoesNotAdoptFencedItemRootWithoutAbortMark: a drain
// replay looks up the unit's item root by gc.item_root_key, closes the
// molecule_failed ones and adopts the first remaining candidate. A client
// killed while instantiating that root leaves the chain fenced with no
// molecule_failed — or, killed inside the activation loop, an activated root
// over fenced steps — so the replay adopts a chain whose steps nothing will
// ever activate, and the drain waits on it.
//
// The one contract asserted: a fenced, never-failed chain is not adopted as
// the unit's item root. A refusal (error) or a row left without a root
// passes; adopting the original chain passes only if each bead carries
// exactly the values it had before the kill (what a healthy activation
// restores); adopting a fresh replacement passes if that chain is live.
// Retiring the abandoned chain is a separate contract and is not asserted
// here; the fixture carries no age or ownership signal.
func TestProcessDrainReplayDoesNotAdoptFencedItemRootWithoutAbortMark(t *testing.T) {
	cases := []struct {
		name      string
		fenceRoot bool
	}{
		{name: "item root and step fenced", fenceRoot: true},
		{name: "item root activated, step fenced", fenceRoot: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			formulatest.EnableV2ForTest(t)
			dir := t.TempDir()
			writeDrainItemFormula(t, dir)
			store, drain := seedDrainWorkflow(t)

			if _, err := ProcessControl(store, drain, ProcessOptions{FormulaSearchPaths: []string{dir}}); err != nil {
				t.Fatalf("ProcessControl(drain expand): %v", err)
			}
			drain = mustGetBead(t, store, drain.ID)
			manifest := mustDrainManifest(t, drain)
			itemRootID := manifest.Rows[0].ItemRootID
			if itemRootID == "" {
				t.Fatal("expand left the first row without an item root")
			}
			before := map[string]beads.Bead{itemRootID: mustGetBead(t, store, itemRootID)}
			if members, err := store.ListByMetadata(map[string]string{beadmeta.RootBeadIDMetadataKey: itemRootID}, 0); err != nil {
				t.Fatalf("list members of %s: %v", itemRootID, err)
			} else {
				for _, m := range members {
					before[m.ID] = m
				}
			}
			fenceLikeKilledInstantiation(t, store, itemRootID, tc.fenceRoot)
			// The client died before the row was persisted with its root: the
			// manifest still says the unit exists and the item root does not.
			manifest.Rows[0].ItemRootID = ""
			manifest.Rows[0].Status = "unit-created"
			if err := persistDrainManifest(store, drain.ID, manifest, map[string]string{"gc.drain_state": "expanding"}); err != nil {
				t.Fatalf("persist rewound manifest: %v", err)
			}

			if _, err := ProcessControl(store, mustGetBead(t, store, drain.ID), ProcessOptions{FormulaSearchPaths: []string{dir}}); err != nil {
				return // refused: not an adoption
			}
			replayed := mustDrainManifest(t, mustGetBead(t, store, drain.ID))
			got := replayed.Rows[0].ItemRootID
			if got == "" {
				return // not adopted
			}
			adopted := mustGetBead(t, store, got)
			members, err := store.ListByMetadata(map[string]string{beadmeta.RootBeadIDMetadataKey: got}, 0, beads.IncludeClosed)
			if err != nil {
				t.Fatalf("list members of %s: %v", got, err)
			}
			for _, b := range append([]beads.Bead{adopted}, members...) {
				live := liveWorkflowBead(b)
				if was, ok := before[b.ID]; ok && got == itemRootID {
					// The original chain was adopted: each bead must carry exactly
					// what a healthy activation restores.
					live = live && b.Type == was.Type && b.Assignee == was.Assignee &&
						b.Metadata[beadmeta.RoutedToMetadataKey] == was.Metadata[beadmeta.RoutedToMetadataKey] &&
						b.Metadata[beadmeta.ExecutionRoutedToMetadataKey] == was.Metadata[beadmeta.ExecutionRoutedToMetadataKey]
				}
				if !live {
					t.Fatalf("drain replay adopted item root %s while bead %s of that chain is not live (status=%q type=%q gc.instantiating=%q molecule_failed=%q deferred_type=%q): ensureDrainItemRoot closes only molecule_failed roots and returns the first remaining candidate, so a chain whose instantiation was killed becomes the unit's item root and the drain waits on it",
						got, b.ID, b.Status, b.Type, b.Metadata[beadmeta.InstantiatingMetadataKey], b.Metadata[beadmeta.MoleculeFailedMetadataKey], b.Metadata[beadmeta.DeferredTypeMetadataKey])
				}
			}
		})
	}
}
