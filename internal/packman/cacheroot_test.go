package packman

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/fsys"
)

// TestSyncLockRecordsActiveCacheRoot pins that the code which materializes the
// closure is also the code that records where it put it — and that the record
// is machine-local city state, never packs.lock (a committed artifact that must
// stay byte-identical across machines).
func TestSyncLockRecordsActiveCacheRoot(t *testing.T) {
	home := t.TempDir()
	city := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GC_HOME", filepath.Join(home, ".gc"))
	stubCachedPackGit(t)

	stageCachedPack(t, "https://example.com/a.git", "aaaa", "[pack]\nname = \"a\"\nschema = 1\n")
	if err := WriteLockfile(fsys.OSFS{}, city, &Lockfile{
		Packs: map[string]LockedPack{
			"https://example.com/a.git": {Version: "sha:aaaa", Commit: "aaaa", Fetched: time.Unix(10, 0).UTC()},
		},
	}); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	if _, err := SyncLock(city, map[string]config.Import{
		"a": {Source: "https://example.com/a.git", Version: "sha:aaaa"},
	}, InstallFromLock); err != nil {
		t.Fatalf("SyncLock: %v", err)
	}

	want, err := RepoCacheRoot()
	if err != nil {
		t.Fatalf("RepoCacheRoot: %v", err)
	}
	if got := config.ReadRepoCacheBinding(city); got != want {
		t.Fatalf("recorded cache root = %q, want %q", got, want)
	}
	raw, err := os.ReadFile(filepath.Join(city, LockfileName))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(raw), want) {
		t.Fatalf("packs.lock must stay free of machine-local paths:\n%s", raw)
	}
}

// TestSyncLockRefusesForeignCacheRoot is the qc-myfj1 regression at the unit
// level: a lock installed under one repo cache root must not be advanced by a
// process resolving a different one. Before the binding check, syncLock happily
// re-resolved the pins and rewrote packs.lock while the clones landed in the
// other root — the "locked but not cached" wedge.
func TestSyncLockRefusesForeignCacheRoot(t *testing.T) {
	home := t.TempDir()
	city := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GC_HOME", filepath.Join(home, "active-gc-home"))
	stubCachedPackGit(t)

	if err := WriteLockfile(fsys.OSFS{}, city, &Lockfile{
		Packs: map[string]LockedPack{
			"https://example.com/a.git": {Version: "sha:aaaa", Commit: "aaaa", Fetched: time.Unix(10, 0).UTC()},
		},
	}); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}
	if err := config.WriteRepoCacheBinding(city, filepath.Join(home, "other-gc-home", "cache", "repos")); err != nil {
		t.Fatalf("WriteRepoCacheBinding: %v", err)
	}
	before, err := os.ReadFile(filepath.Join(city, LockfileName))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	_, err = SyncLock(city, map[string]config.Import{
		"a": {Source: "https://example.com/a.git", Version: "sha:bbbb"},
	}, InstallResolveIfNeeded)
	if !errors.Is(err, ErrCacheRootMismatch) {
		t.Fatalf("SyncLock error = %v, want ErrCacheRootMismatch", err)
	}
	var mismatch *CacheRootMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error does not carry CacheRootMismatchError: %v", err)
	}
	for _, want := range []string{mismatch.BoundRoot, mismatch.ActiveRoot} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not name root %q", err, want)
		}
	}

	after, err := os.ReadFile(filepath.Join(city, LockfileName))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("packs.lock advanced despite the refusal:\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// TestInstallLockedRefusesForeignCacheRoot covers the other entry point: the
// doctor/rig/preflight paths call InstallLocked directly.
func TestInstallLockedRefusesForeignCacheRoot(t *testing.T) {
	home := t.TempDir()
	city := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GC_HOME", filepath.Join(home, "active-gc-home"))
	stubCachedPackGit(t)

	if err := WriteLockfile(fsys.OSFS{}, city, &Lockfile{
		Packs: map[string]LockedPack{
			"https://example.com/a.git": {Version: "sha:aaaa", Commit: "aaaa", Fetched: time.Unix(10, 0).UTC()},
		},
	}); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}
	if err := config.WriteRepoCacheBinding(city, filepath.Join(home, "other-gc-home", "cache", "repos")); err != nil {
		t.Fatalf("WriteRepoCacheBinding: %v", err)
	}
	if _, err := InstallLocked(city); !errors.Is(err, ErrCacheRootMismatch) {
		t.Fatalf("InstallLocked error = %v, want ErrCacheRootMismatch", err)
	}
}

// TestRebindCacheRootClearsBinding pins the deliberate escape: after a rebind
// the next sync re-stamps with the active root instead of refusing.
func TestRebindCacheRootClearsBinding(t *testing.T) {
	home := t.TempDir()
	city := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GC_HOME", filepath.Join(home, "active-gc-home"))
	stubCachedPackGit(t)

	if err := WriteLockfile(fsys.OSFS{}, city, &Lockfile{
		Packs: map[string]LockedPack{
			"https://example.com/a.git": {Version: "sha:aaaa", Commit: "aaaa", Fetched: time.Unix(10, 0).UTC()},
		},
	}); err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}
	if err := config.WriteRepoCacheBinding(city, filepath.Join(home, "other-gc-home", "cache", "repos")); err != nil {
		t.Fatalf("WriteRepoCacheBinding: %v", err)
	}
	if err := CheckCacheRootBinding(city); !errors.Is(err, ErrCacheRootMismatch) {
		t.Fatalf("CheckCacheRootBinding error = %v, want ErrCacheRootMismatch", err)
	}
	if err := RebindCacheRoot(city); err != nil {
		t.Fatalf("RebindCacheRoot: %v", err)
	}
	if err := CheckCacheRootBinding(city); err != nil {
		t.Fatalf("CheckCacheRootBinding after rebind: %v", err)
	}
}

// TestCityWithoutRecordedCacheRootIsUnbound pins backward compatibility: a city
// installed before the record existed must keep working everywhere until an
// install records a root for it.
func TestCityWithoutRecordedCacheRootIsUnbound(t *testing.T) {
	home := t.TempDir()
	city := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GC_HOME", filepath.Join(home, ".gc"))
	if err := os.WriteFile(filepath.Join(city, LockfileName), []byte(`schema = 1

[packs."https://example.com/a.git"]
version = "sha:aaaa"
commit = "aaaa"
fetched = "2026-08-21T00:00:00Z"
`), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := CheckCacheRootBinding(city); err != nil {
		t.Fatalf("legacy lock must not be bound: %v", err)
	}
}

// TestGcHomeSplitWedgeIsRefused is the end-to-end qc-myfj1 reproduction against
// real git: a city installed under an explicit GC_HOME, then a pin advance run
// by a process whose GC_HOME is unset (the shape a login-profile export gives
// an agent/non-login shell). The lock must not advance, because the clones
// would land in $HOME/.gc while the city is served from the recorded root.
func TestGcHomeSplitWedgeIsRefused(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	// A closure, not a package helper, on purpose: the resource census
	// attributes calls to their enclosing top-level test, and this test's
	// [[medium]] subprocess declaration in test/test-resources.toml covers
	// exactly the git calls lexically inside it. A shared helper would put
	// the exec.Command site back into the Small debt census.
	gitInTest := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
			"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %s: %v", strings.Join(args, " "), out, err)
		}
		return strings.TrimSpace(string(out))
	}
	home := t.TempDir()
	t.Setenv("HOME", home)

	repo := t.TempDir()
	gitInTest(repo, "init", "-q", "-b", "main")
	writeTestPack(t, repo, "packs/groom")
	gitInTest(repo, "add", "-A")
	gitInTest(repo, "commit", "-qm", "c1")
	c1 := gitInTest(repo, "rev-parse", "HEAD")

	city := t.TempDir()
	source := "file://" + repo + "//packs/groom"
	supervisorHome := filepath.Join(home, "data-gc-home")

	// The supervisor's environment: an explicit GC_HOME.
	t.Setenv("GC_HOME", supervisorHome)
	install := func(pin string) error {
		lock, err := SyncLock(city, map[string]config.Import{
			"rig:qcore:groom": {Source: source, Version: "sha:" + pin},
		}, InstallResolveIfNeeded)
		if err != nil {
			return err
		}
		if err := WriteLockfile(fsys.OSFS{}, city, lock); err != nil {
			return err
		}
		_, err = InstallLocked(city)
		return err
	}
	if err := install(c1); err != nil {
		t.Fatalf("first install: %v", err)
	}
	supervisorRoot := filepath.Join(supervisorHome, "cache", "repos")
	if _, err := os.Stat(filepath.Join(supervisorRoot, RepoCacheKey(source, c1))); err != nil {
		t.Fatalf("first install did not populate the supervisor cache: %v", err)
	}

	// qc-bridge main moves; the operator advances the pin.
	writeTestPack(t, repo, "packs/groom")
	if err := os.WriteFile(filepath.Join(repo, "packs/groom", "README.md"), []byte("advanced\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitInTest(repo, "add", "-A")
	gitInTest(repo, "commit", "-qm", "c2")
	c2 := gitInTest(repo, "rev-parse", "HEAD")

	// The agent shell: GC_HOME never made it into this environment, so
	// ImplicitGCHome falls back to $HOME/.gc. A test binary reports "" rather
	// than that fallback (the hermeticity guard in ImplicitGCHome), so set the
	// exact path production would compute instead of unsetting the variable.
	t.Setenv("GC_HOME", filepath.Join(home, ".gc"))
	err := install(c2)
	if !errors.Is(err, ErrCacheRootMismatch) {
		t.Fatalf("install from a foreign cache root: err = %v, want ErrCacheRootMismatch", err)
	}

	onDisk, err := ReadLockfile(fsys.OSFS{}, city)
	if err != nil {
		t.Fatalf("ReadLockfile: %v", err)
	}
	if got := onDisk.Packs[source].Commit; got != c1 {
		t.Fatalf("packs.lock advanced to %s despite the refusal (want %s)", got, c1)
	}
	if got := config.ReadRepoCacheBinding(city); got != supervisorRoot {
		t.Fatalf("recorded cache root = %q, want %q", got, supervisorRoot)
	}
	// And the supervisor's cache is untouched by the foreign run.
	if _, err := os.Stat(filepath.Join(supervisorRoot, RepoCacheKey(source, c2))); !os.IsNotExist(err) {
		t.Fatalf("supervisor cache gained a clone it never asked for: %v", err)
	}
}

func writeTestPack(t *testing.T, root, sub string) {
	t.Helper()
	dir := filepath.Join(root, sub)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pack.toml"), []byte("[pack]\nname = \"groom\"\nschema = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
