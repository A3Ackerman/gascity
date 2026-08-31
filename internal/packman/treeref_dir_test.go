package packman

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A cache checkout for a tree URL whose branch name contains "/" — the parse
// layer cannot know the ref/path boundary, so the pack-dir resolution must
// recover it from the cache's own refs. The fixture writes the ref files a
// fetched clone carries; no git subprocess is involved.
func newSlashRefCache(t *testing.T) string {
	t.Helper()
	cache := t.TempDir()
	if err := os.MkdirAll(filepath.Join(cache, ".git", "refs"), 0o755); err != nil {
		t.Fatal(err)
	}
	packed := "# pack-refs with: peeled fully-peeled sorted\n" +
		"0123456789abcdef0123456789abcdef01234567 refs/remotes/origin/interim/box-dog\n" +
		"0123456789abcdef0123456789abcdef01234567 refs/remotes/origin/main\n"
	if err := os.WriteFile(filepath.Join(cache, ".git", "packed-refs"), []byte(packed), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cache, "examples", "bd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "examples", "bd", "pack.toml"), []byte("[pack]\nname = \"bd\"\nschema = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return cache
}

func TestResolveCachedPackDirRecoversSlashRefBoundary(t *testing.T) {
	cache := newSlashRefCache(t)
	source := "https://github.com/example/repo/tree/interim/box-dog/examples/bd"

	dir, err := resolveCachedPackDir(source, cache)
	if err != nil {
		t.Fatalf("resolveCachedPackDir: %v", err)
	}
	want := filepath.Join(cache, "examples", "bd")
	if dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
}

func TestResolveCachedPackDirKeepsPlainSourcesUntouched(t *testing.T) {
	cache := newSlashRefCache(t)
	source := "https://github.com/example/repo/tree/main/examples/bd"

	dir, err := resolveCachedPackDir(source, cache)
	if err != nil {
		t.Fatalf("resolveCachedPackDir: %v", err)
	}
	if want := filepath.Join(cache, "examples", "bd"); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
}

// A proven mis-split — the refs name a slash ref, and neither boundary
// yields a pack — must refuse naming the ambiguity and the #ref escape,
// never a bare missing-pack.toml with a mis-split path.
func TestResolveCachedPackDirRefusesLoudlyWhenNoBoundaryWorks(t *testing.T) {
	cache := newSlashRefCache(t)
	source := "https://github.com/example/repo/tree/interim/box-dog/no/such/dir"

	_, err := resolveCachedPackDir(source, cache)
	if err == nil {
		t.Fatal("expected a loud refusal")
	}
	for _, want := range []string{"interim/box-dog", "#"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err.Error(), want)
		}
	}
}

// An ordinary miss with no recovery evidence keeps the naive resolution so
// the existing missing-pack diagnostics report as they always have.
func TestResolveCachedPackDirOrdinaryMissKeepsNaivePath(t *testing.T) {
	cache := t.TempDir() // no .git at all — no recovery evidence
	source := "https://github.com/example/repo/tree/main/gastown"

	dir, err := resolveCachedPackDir(source, cache)
	if err != nil {
		t.Fatalf("resolveCachedPackDir: %v", err)
	}
	if want := filepath.Join(cache, "gastown"); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
}
