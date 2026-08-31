package packman

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// A cache checkout for a tree URL whose branch name contains "/" — the parse
// layer cannot know the ref/path boundary, so the pack-dir resolution must
// recover it from the cache's own refs.
func newSlashRefCache(t *testing.T) string {
	t.Helper()
	cache := t.TempDir()
	gitIn(t, cache, "init", "--quiet")
	if err := os.MkdirAll(filepath.Join(cache, "examples", "bd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cache, "examples", "bd", "pack.toml"), []byte("[pack]\nname = \"bd\"\nschema = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, cache, "add", ".")
	gitIn(t, cache, "commit", "--quiet", "-m", "seed")
	// The fetched cache carries the remote's branches as remote-tracking refs.
	gitIn(t, cache, "update-ref", "refs/remotes/origin/interim/box-dog", "HEAD")
	gitIn(t, cache, "update-ref", "refs/remotes/origin/main", "HEAD")
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

// When no boundary yields a pack.toml, the refusal must name the ambiguity
// and the #ref escape — never a bare missing-pack.toml with a mis-split path.
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
