package git

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func writeRepoFixture(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitDir := filepath.Join(repo, ".git")
	for _, dir := range []string{
		filepath.Join(gitDir, "refs", "heads"),
		filepath.Join(gitDir, "refs", "tags"),
		filepath.Join(gitDir, "refs", "remotes", "origin", "interim"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	sha := "0123456789abcdef0123456789abcdef01234567\n"
	// Loose refs: a local head, a slash-named remote-tracking branch, and the
	// remote HEAD symref that must not surface as a branch name.
	if err := os.WriteFile(filepath.Join(gitDir, "refs", "heads", "local-work"), []byte(sha), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "refs", "remotes", "origin", "interim", "box-dog"), []byte(sha), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "refs", "remotes", "origin", "HEAD"), []byte("ref: refs/remotes/origin/main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Packed refs: a remote-tracking branch, and an annotated tag with its
	// peel line, plus the comment header packed-refs files carry.
	packed := "# pack-refs with: peeled fully-peeled sorted\n" +
		"0123456789abcdef0123456789abcdef01234567 refs/remotes/origin/main\n" +
		"0123456789abcdef0123456789abcdef01234567 refs/tags/v1.2.3\n" +
		"^fedcba9876543210fedcba9876543210fedcba98\n"
	if err := os.WriteFile(filepath.Join(gitDir, "packed-refs"), []byte(packed), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

// LocalRefNames reads a repository's branch and tag names from disk — packed
// and loose refs both — with remote-tracking names stripped of their remote
// segment, so a tree URL's ref/path boundary can be recovered without
// shelling out or touching the network.
func TestLocalRefNames(t *testing.T) {
	repo := writeRepoFixture(t)
	got, err := LocalRefNames(repo)
	if err != nil {
		t.Fatalf("LocalRefNames: %v", err)
	}
	sort.Strings(got)
	want := []string{"interim/box-dog", "local-work", "main", "v1.2.3"}
	if len(got) != len(want) {
		t.Fatalf("refs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("refs = %v, want %v", got, want)
		}
	}
}

// A worktree-style .git FILE points at the real git dir; refs resolve
// through it.
func TestLocalRefNamesFollowsGitdirPointer(t *testing.T) {
	realRepo := writeRepoFixture(t)
	linked := t.TempDir()
	pointer := "gitdir: " + filepath.Join(realRepo, ".git") + "\n"
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte(pointer), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LocalRefNames(linked)
	if err != nil {
		t.Fatalf("LocalRefNames: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("expected refs through the gitdir pointer")
	}
}

func TestLocalRefNamesMissingRepo(t *testing.T) {
	if _, err := LocalRefNames(t.TempDir()); err == nil {
		t.Fatal("expected an error for a directory with no .git")
	}
}
