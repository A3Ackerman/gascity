package beads

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	beadslib "github.com/steveyegge/beads"
)

// lostUpdateStorage models the one Dolt property the metadata write path
// depends on: a statement outside a transaction replaces the row it names,
// while a transaction that read the row fails at write time with a
// serialization conflict if the row changed after its read. A concurrent
// update lands exactly once, right after the first read the store performs,
// which is the interleaving observed in the audit trail (the fence activation
// committing between a metadata read and its write-back).
type lostUpdateStorage struct {
	beadslib.Storage
	durable    *beadslib.Issue
	version    int
	concurrent map[string]string
	fired      bool
}

func (s *lostUpdateStorage) fireConcurrentUpdate() {
	if s.fired {
		return
	}
	s.fired = true
	s.durable.Metadata = mergeNativeMetadataForTest(s.durable.Metadata, s.concurrent)
	s.version++
}

func (s *lostUpdateStorage) GetIssue(context.Context, string) (*beadslib.Issue, error) {
	snapshot := cloneNativeIssueForTest(s.durable)
	s.fireConcurrentUpdate()
	return snapshot, nil
}

func (s *lostUpdateStorage) UpdateIssue(_ context.Context, _ string, updates map[string]interface{}, _ string) error {
	raw, ok := updates["metadata"].(json.RawMessage)
	if !ok {
		return errors.New("metadata update is not json.RawMessage")
	}
	s.durable.Metadata = append(json.RawMessage(nil), raw...)
	s.version++
	return nil
}

func (s *lostUpdateStorage) RunInTransaction(_ context.Context, _ string, fn func(beadslib.Transaction) error) error {
	return fn(&lostUpdateTx{storage: s, readVersion: -1})
}

type lostUpdateTx struct {
	beadslib.Transaction
	storage     *lostUpdateStorage
	readVersion int
}

func (t *lostUpdateTx) GetIssue(context.Context, string) (*beadslib.Issue, error) {
	snapshot := cloneNativeIssueForTest(t.storage.durable)
	t.readVersion = t.storage.version
	t.storage.fireConcurrentUpdate()
	return snapshot, nil
}

func (t *lostUpdateTx) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	if t.readVersion != t.storage.version {
		return errors.New("this transaction conflicts with a committed transaction")
	}
	return t.storage.UpdateIssue(ctx, id, updates, actor)
}

func mergeNativeMetadataForTest(raw json.RawMessage, kvs map[string]string) json.RawMessage {
	metadata := map[string]string{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &metadata); err != nil {
			panic(err)
		}
	}
	for k, v := range kvs {
		metadata[k] = v
	}
	out, err := json.Marshal(metadata)
	if err != nil {
		panic(err)
	}
	return out
}

// TestNativeDoltStoreMetadataWriteKeepsAConcurrentUpdate pins that a metadata
// write never undoes an update that committed between its read and its
// write. The store merges the new keys into the map it read and writes the
// whole map back, so outside a transaction that write-back replaces every key
// the concurrent update touched with the values from before it. The
// interleaving here is the audit trail of a lost review workflow: a graph
// step's fence activation (routed_to restored, fence keys cleared) committed
// between a heartbeat-shaped stamp's read and its write, and the stamp put the
// fence back.
func TestNativeDoltStoreMetadataWriteKeepsAConcurrentUpdate(t *testing.T) {
	const id = "gc-fenced-step"
	activation := map[string]string{
		"gc.instantiating":      "",
		"gc.deferred_routed_to": "",
		"gc.routed_to":          "rig/pool",
	}
	newStorage := func() *lostUpdateStorage {
		return &lostUpdateStorage{
			durable: &beadslib.Issue{
				ID:        id,
				Title:     "Resolve PR identity",
				Status:    beadslib.StatusOpen,
				IssueType: beadslib.TypeTask,
				Priority:  2,
				Metadata:  json.RawMessage(`{"gc.run_target":"pool","gc.instantiating":"true","gc.deferred_routed_to":"rig/pool"}`),
			},
			concurrent: activation,
		}
	}
	assertKept := func(t *testing.T, storage *lostUpdateStorage, key, want string) {
		t.Helper()
		metadata, err := metadataMapFromNative(storage.durable.Metadata)
		if err != nil {
			t.Fatalf("parse durable metadata: %v", err)
		}
		if got := metadata[key]; got != want {
			t.Fatalf("%s = %q after the metadata write, want %q (the concurrent update was overwritten by the stale map)", key, got, want)
		}
	}

	t.Run("SetMetadataBatch", func(t *testing.T) {
		storage := newStorage()
		store := newNativeDoltStoreForTest(storage)
		if err := store.SetMetadataBatch(id, map[string]string{"gc.heartbeat": "now"}); err != nil {
			t.Fatalf("SetMetadataBatch: %v", err)
		}
		if !storage.fired {
			t.Fatal("the concurrent update never landed; the interleaving was not exercised")
		}
		assertKept(t, storage, "gc.heartbeat", "now")
		assertKept(t, storage, "gc.instantiating", "")
		assertKept(t, storage, "gc.deferred_routed_to", "")
		assertKept(t, storage, "gc.routed_to", "rig/pool")
	})

	t.Run("SetMetadata", func(t *testing.T) {
		storage := newStorage()
		store := newNativeDoltStoreForTest(storage)
		if err := store.SetMetadata(id, "gc.heartbeat", "now"); err != nil {
			t.Fatalf("SetMetadata: %v", err)
		}
		assertKept(t, storage, "gc.heartbeat", "now")
		assertKept(t, storage, "gc.instantiating", "")
		assertKept(t, storage, "gc.routed_to", "rig/pool")
	})
}
