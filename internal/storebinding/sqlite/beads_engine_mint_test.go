package sqlite

// The prefix an opened engine mints under.
//
// A binding mints from one id sequence under one reserved prefix, and the
// prefix is the LEAD class's (storebinding.ClassSet.MintClass): graph whenever
// the binding serves it, so a whole split keeps minting gcg exactly as every
// converged city already holds; the sole class otherwise, so a binding serving
// messaging alone mints gcm and its ids can never be read as the work store's
// — or as the graph binding's.

import (
	"errors"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/coordclass"
	"github.com/gastownhall/gascity/internal/storebinding"
)

func openEngineFor(t *testing.T, classes ...coordclass.Class) (beads.Store, error) {
	t.Helper()
	spec := beadsTestSpec(t.TempDir())
	provider := newBeadsProvider(t, spec)
	opener, ok := provider.(storebinding.EngineOpener)
	if !ok {
		t.Fatal("the Beads provider does not open an engine")
	}
	store, closer, err := opener.OpenEngine(spec, classSet(t, classes...))
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() {
		if err := closer.Close(); err != nil {
			t.Errorf("closing the engine: %v", err)
		}
	})
	return store, nil
}

func mintedPrefix(t *testing.T, store beads.Store) string {
	t.Helper()
	declaring, ok := store.(interface{ IDPrefix() string })
	if !ok {
		t.Fatalf("the opened engine (%T) declares no id prefix", store)
	}
	return declaring.IDPrefix()
}

func TestOpenEngineMintsUnderTheLeadClassPrefix(t *testing.T) {
	for name, tc := range map[string]struct {
		classes []coordclass.Class
		class   string
	}{
		"the whole split mints graph ids": {
			[]coordclass.Class{coordclass.ClassGraph, coordclass.ClassSessions, coordclass.ClassMessaging, coordclass.ClassOrders, coordclass.ClassNudges},
			config.BeadClassGraph,
		},
		"a messaging-only binding mints messaging ids": {
			[]coordclass.Class{coordclass.ClassMessaging},
			config.BeadClassMessaging,
		},
		"an orders-only binding mints order ids": {
			[]coordclass.Class{coordclass.ClassOrders},
			config.BeadClassOrders,
		},
	} {
		t.Run(name, func(t *testing.T) {
			want, ok := config.ReservedClassPrefix(tc.class)
			if !ok {
				t.Fatalf("no reserved prefix for %q", tc.class)
			}
			store, err := openEngineFor(t, tc.classes...)
			if err != nil {
				t.Fatalf("opening the engine: %v", err)
			}
			if got := mintedPrefix(t, store); got != want {
				t.Fatalf("the engine declares prefix %q, want %q", got, want)
			}
			created, err := store.Create(beads.Bead{Title: "minted through the binding", Type: "message"})
			if err != nil {
				t.Fatalf("creating through the engine: %v", err)
			}
			if !strings.HasPrefix(created.ID, want+"-") {
				t.Fatalf("the engine minted %q, want an id under %q", created.ID, want)
			}
		})
	}
}

// TestOpenEngineRefusesAClassSetWithNoLeadClass: several infrastructure classes
// without graph have no single prefix to mint under, and the engine refuses at
// the open rather than minting one class's namespace for another's beads.
func TestOpenEngineRefusesAClassSetWithNoLeadClass(t *testing.T) {
	_, err := openEngineFor(t, coordclass.ClassMessaging, coordclass.ClassOrders)
	if !errors.Is(err, ErrInvalidBeadsBinding) {
		t.Fatalf("OpenEngine on {messaging, orders} = %v, want %v", err, ErrInvalidBeadsBinding)
	}
	if !strings.Contains(err.Error(), "prefix") {
		t.Errorf("the refusal does not say the binding has no single prefix to mint under: %v", err)
	}
}
