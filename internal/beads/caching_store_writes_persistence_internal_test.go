package beads

import (
	"context"
	"errors"
	"testing"
)

// These tests reuse refreshFailingStore (caching_store_internal_test.go): it
// fails exactly one Get and is otherwise an honest pass-through, so every write
// it accepts is applied to the wrapped store. Only the post-write refresh read
// fails, which is what the cache already logs through recordProblem.

// failingBatchStore rejects SetMetadataBatch outright while fail is set.
type failingBatchStore struct {
	Store
	fail bool
}

func (s *failingBatchStore) SetMetadataBatch(id string, kvs map[string]string) error {
	if s.fail {
		return errors.New("backing rejected the batch")
	}
	return s.Store.SetMetadataBatch(id, kvs)
}

func primedCacheOver(t *testing.T, backing Store) *CachingStore {
	t.Helper()

	cache := NewCachingStoreForTest(backing, nil)
	if err := cache.Prime(context.Background()); err != nil {
		t.Fatalf("Prime: %v", err)
	}
	return cache
}

func assertBackingHoldsPatch(t *testing.T, backing Store, id string, patch map[string]string) {
	t.Helper()

	got, err := backing.Get(id)
	if err != nil {
		t.Fatalf("backing.Get(%s): %v", id, err)
	}
	for k, want := range patch {
		if got.Metadata[k] != want {
			t.Errorf("write returned nil but backing metadata[%q] = %q; want %q (backing row: %v)",
				k, got.Metadata[k], want, got.Metadata)
		}
	}
}

// TestCachingStoreSetMetadataBatchNeverSkipsAWriteTheBackingNeverSaw pins the
// contract behind the idempotence short-circuit: a nil return means the backing
// store holds every key/value of that patch. The short-circuit may only claim a
// no-op from a row it read back from the backing, never from a row it folded
// locally because the post-write refresh read failed.
func TestCachingStoreSetMetadataBatchNeverSkipsAWriteTheBackingNeverSaw(t *testing.T) {
	t.Run("batch retry after an unverified fold", func(t *testing.T) {
		t.Parallel()

		mem := NewMemStore()
		failing := &refreshFailingStore{Store: mem}
		backing := &countingBackingStore{Store: failing}
		bead := createBeadWithMetadata(t, mem, map[string]string{"generation": "1"})
		cache := primedCacheOver(t, backing)

		failing.failNextGet = true
		if err := cache.SetMetadataBatch(bead.ID, map[string]string{"generation": "2"}); err != nil {
			t.Fatalf("SetMetadataBatch: %v", err)
		}
		// Another writer in the city moves the row on. The cache never saw it:
		// its own row is the guess it folded when the refresh failed.
		if err := mem.SetMetadata(bead.ID, "generation", "3"); err != nil {
			t.Fatalf("external SetMetadata: %v", err)
		}

		patch := map[string]string{"generation": "2"}
		calls := backing.setMetadataBatchCalls
		if err := cache.SetMetadataBatch(bead.ID, patch); err != nil {
			t.Fatalf("retry SetMetadataBatch: %v", err)
		}
		if backing.setMetadataBatchCalls == calls {
			t.Errorf("retry short-circuited against an unverified cached row; backing.SetMetadataBatch not called")
		}
		assertBackingHoldsPatch(t, mem, bead.ID, patch)
	})

	t.Run("single-key retry after an unverified fold", func(t *testing.T) {
		t.Parallel()

		mem := NewMemStore()
		failing := &refreshFailingStore{Store: mem}
		backing := &countingBackingStore{Store: failing}
		bead := createBeadWithMetadata(t, mem, map[string]string{"generation": "1"})
		cache := primedCacheOver(t, backing)

		failing.failNextGet = true
		if err := cache.SetMetadata(bead.ID, "generation", "2"); err != nil {
			t.Fatalf("SetMetadata: %v", err)
		}
		if err := mem.SetMetadata(bead.ID, "generation", "3"); err != nil {
			t.Fatalf("external SetMetadata: %v", err)
		}

		calls := backing.setMetadataCalls
		if err := cache.SetMetadata(bead.ID, "generation", "2"); err != nil {
			t.Fatalf("retry SetMetadata: %v", err)
		}
		if backing.setMetadataCalls == calls {
			t.Errorf("retry short-circuited against an unverified cached row; backing.SetMetadata not called")
		}
		assertBackingHoldsPatch(t, mem, bead.ID, map[string]string{"generation": "2"})
	})

	t.Run("reads do not serve an unverified fold", func(t *testing.T) {
		t.Parallel()

		mem := NewMemStore()
		failing := &refreshFailingStore{Store: mem}
		bead := createBeadWithMetadata(t, mem, map[string]string{"generation": "1"})
		cache := primedCacheOver(t, failing)

		failing.failNextGet = true
		if err := cache.SetMetadataBatch(bead.ID, map[string]string{"generation": "2"}); err != nil {
			t.Fatalf("SetMetadataBatch: %v", err)
		}
		if err := mem.SetMetadata(bead.ID, "generation", "3"); err != nil {
			t.Fatalf("external SetMetadata: %v", err)
		}

		got, err := cache.Get(bead.ID)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if got.Metadata["generation"] != "3" {
			t.Errorf("Get served generation=%q from an unverified fold; want %q from the backing",
				got.Metadata["generation"], "3")
		}
	})

	t.Run("retry after a rejected batch still reaches the backing", func(t *testing.T) {
		t.Parallel()

		backing := &failingBatchStore{Store: NewMemStore(), fail: true}
		bead := createBeadWithMetadata(t, backing.Store, map[string]string{"generation": "1"})
		cache := primedCacheOver(t, backing)

		patch := map[string]string{"generation": "2"}
		if err := cache.SetMetadataBatch(bead.ID, patch); err == nil {
			t.Fatalf("SetMetadataBatch: want the backing's error")
		}
		backing.fail = false
		if err := cache.SetMetadataBatch(bead.ID, map[string]string{"generation": "2"}); err != nil {
			t.Fatalf("retry SetMetadataBatch: %v", err)
		}
		assertBackingHoldsPatch(t, backing.Store, bead.ID, patch)
	})
}

// TestCachingStoreMetadataNoOpSuppressionSurvivesTheFence pins the other half of
// the contract: the fence is scoped to rows the cache could not verify, so the
// no-op suppression that keeps reconciler ticks from minting a bead.updated per
// re-stamped marker still fires for rows absorbed from a read — including after
// an ordinary read re-verifies a row that was fenced.
func TestCachingStoreMetadataNoOpSuppressionSurvivesTheFence(t *testing.T) {
	t.Run("row absorbed from a read", func(t *testing.T) {
		t.Parallel()

		backing := &countingBackingStore{Store: NewMemStore()}
		bead := createBeadWithMetadata(t, backing.Store, map[string]string{"marker": "1"})
		cache := primedCacheOver(t, backing)

		if err := cache.SetMetadataBatch(bead.ID, map[string]string{"marker": "2"}); err != nil {
			t.Fatalf("SetMetadataBatch: %v", err)
		}
		backing.setMetadataBatchCalls = 0
		backing.setMetadataCalls = 0
		if err := cache.SetMetadataBatch(bead.ID, map[string]string{"marker": "2"}); err != nil {
			t.Fatalf("re-stamp SetMetadataBatch: %v", err)
		}
		if err := cache.SetMetadata(bead.ID, "marker", "2"); err != nil {
			t.Fatalf("re-stamp SetMetadata: %v", err)
		}
		if backing.setMetadataBatchCalls != 0 || backing.setMetadataCalls != 0 {
			t.Errorf("no-op re-stamp reached the backing: batch=%d single=%d; want 0/0",
				backing.setMetadataBatchCalls, backing.setMetadataCalls)
		}
	})

	t.Run("fenced row after a read re-verifies it", func(t *testing.T) {
		t.Parallel()

		mem := NewMemStore()
		failing := &refreshFailingStore{Store: mem}
		backing := &countingBackingStore{Store: failing}
		bead := createBeadWithMetadata(t, mem, map[string]string{"marker": "1"})
		cache := primedCacheOver(t, backing)

		failing.failNextGet = true
		if err := cache.SetMetadataBatch(bead.ID, map[string]string{"marker": "2"}); err != nil {
			t.Fatalf("SetMetadataBatch: %v", err)
		}
		if _, err := cache.Get(bead.ID); err != nil {
			t.Fatalf("Get: %v", err)
		}

		backing.setMetadataBatchCalls = 0
		if err := cache.SetMetadataBatch(bead.ID, map[string]string{"marker": "2"}); err != nil {
			t.Fatalf("re-stamp SetMetadataBatch: %v", err)
		}
		if backing.setMetadataBatchCalls != 0 {
			t.Errorf("no-op re-stamp reached the backing after the row was re-verified: %d calls; want 0",
				backing.setMetadataBatchCalls)
		}
	})
}
