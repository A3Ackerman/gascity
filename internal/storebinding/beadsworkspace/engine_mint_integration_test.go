//go:build integration

package beadsworkspace

// The prefix a workspace binding is required to mint under, against a real
// workspace: the LEAD class's reserved prefix (storebinding.ClassSet.MintClass).
// A binding serving messaging alone requires gcm — a workspace provisioned
// for the whole split, on gcg, is refused for it by name.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/coordclass"
	"github.com/gastownhall/gascity/internal/storebinding"
)

func messagingOnly(t *testing.T) storebinding.ClassSet {
	t.Helper()
	classes, err := storebinding.NewClassSet(coordclass.ClassMessaging)
	if err != nil {
		t.Fatalf("building the messaging-only class set: %v", err)
	}
	return classes
}

// TestOpenEngineServesAMessagingOnlyBindingFromAGcmWorkspace is the serving
// claim for the single-class shape: a workspace that mints gcm serves a binding
// assigned messaging alone, and a mail bead written through it is in the
// workspace afterwards under a gcm id.
func TestOpenEngineServesAMessagingOnlyBindingFromAGcmWorkspace(t *testing.T) {
	city, provider, spec := cityWithProvider(t)
	root, err := WorkspaceRoot(city, testConfigRef)
	if err != nil {
		t.Fatalf("resolving the workspace root: %v", err)
	}
	makeWorkspace(t, root, "gcm")

	store, closer, err := engineOpener(t, provider).OpenEngine(spec, messagingOnly(t))
	if err != nil {
		t.Fatalf("opening a gcm workspace for a messaging-only binding: %v", err)
	}
	created, err := store.Create(beads.Bead{Title: "mail served from the workspace", Type: "message"})
	if err != nil {
		_ = closer.Close()
		t.Fatalf("writing mail through the workspace binding: %v", err)
	}
	if err := closer.Close(); err != nil {
		t.Fatalf("closing the workspace binding: %v", err)
	}
	if !strings.HasPrefix(created.ID, "gcm-") {
		t.Fatalf("the workspace minted %q, want a gcm id", created.ID)
	}

	reopened, err := beads.OpenNativeDoltStoreAt(context.Background(), root, nil)
	if err != nil {
		t.Fatalf("reopening the workspace at %s: %v", root, err)
	}
	t.Cleanup(func() {
		if err := reopened.CloseStore(); err != nil {
			t.Errorf("closing the reopened workspace: %v", err)
		}
	})
	if _, err := reopened.Get(created.ID); err != nil {
		t.Fatalf("reading %s back from the workspace: %v", created.ID, err)
	}
}

// TestOpenEngineRefusesAGcgWorkspaceForAMessagingOnlyBinding: the whole
// split's workspace mints gcg, and a messaging-only binding must not be
// served from it — its ids would read as the graph binding's. The refusal
// names the prefix the binding requires.
func TestOpenEngineRefusesAGcgWorkspaceForAMessagingOnlyBinding(t *testing.T) {
	city, provider, spec := cityWithProvider(t)
	root, err := WorkspaceRoot(city, testConfigRef)
	if err != nil {
		t.Fatalf("resolving the workspace root: %v", err)
	}
	makeWorkspace(t, root, "gcg")

	store, closer, err := engineOpener(t, provider).OpenEngine(spec, messagingOnly(t))
	if !errors.Is(err, ErrInvalidWorkspaceBinding) {
		if closer != nil {
			_ = closer.Close()
		}
		t.Fatalf("OpenEngine on a gcg workspace for messaging alone = %v, want %v", err, ErrInvalidWorkspaceBinding)
	}
	if store != nil || closer != nil {
		t.Fatal("a refused open returned a store or a closer")
	}
	if !strings.Contains(err.Error(), `"gcm"`) {
		t.Errorf("the refusal does not name the prefix a messaging-only binding requires: %v", err)
	}
}
