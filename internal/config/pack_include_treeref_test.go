package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func slashRefCacheDir(t *testing.T) string {
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

// The locked-import read path must serve a slash-named-ref tree source from
// the cache the install populated: the naive first-slash split points at a
// directory that does not exist, and re-deriving it on every config load is
// what left the city in unresolved-import drift after a green install.
func TestCachedIncludeDirRecoversSlashRefBoundary(t *testing.T) {
	cache := slashRefCacheDir(t)
	source := "https://github.com/example/repo/tree/interim/box-dog/examples/bd"

	dir, err := cachedIncludeDir(source, cache)
	if err != nil {
		t.Fatalf("cachedIncludeDir: %v", err)
	}
	if want := filepath.Join(cache, "examples", "bd"); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
}

func TestCachedIncludeDirKeepsPlainSourcesUntouched(t *testing.T) {
	cache := slashRefCacheDir(t)
	dir, err := cachedIncludeDir("https://github.com/example/repo/tree/main/examples/bd", cache)
	if err != nil {
		t.Fatalf("cachedIncludeDir: %v", err)
	}
	if want := filepath.Join(cache, "examples", "bd"); dir != want {
		t.Fatalf("dir = %q, want %q", dir, want)
	}
}

func TestCachedIncludeDirRefusesLoudlyWhenNoBoundaryWorks(t *testing.T) {
	cache := slashRefCacheDir(t)
	_, err := cachedIncludeDir("https://github.com/example/repo/tree/interim/box-dog/no/such", cache)
	if err == nil {
		t.Fatal("expected a loud refusal")
	}
	if !strings.Contains(err.Error(), "interim") || !strings.Contains(err.Error(), "#") {
		t.Errorf("error %q should name the ambiguous ref and the #ref escape", err.Error())
	}
}
