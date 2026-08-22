package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gastownhall/gascity/internal/citylayout"
)

// repoCacheBindingName is the machine-local record of which shared repo cache
// root a city's packs are materialized in. It lives under the city's .gc/
// runtime root, NOT in packs.lock: the lockfile is a committed artifact that
// must stay byte-identical across machines, while a cache root is an absolute,
// machine-local path that differs per host and per operator.
const repoCacheBindingName = "repo-cache-root"

// RepoCacheBindingPath returns the machine-local cache-root record for cityRoot.
func RepoCacheBindingPath(cityRoot string) string {
	return filepath.Join(cityRoot, citylayout.RuntimeRoot, repoCacheBindingName)
}

// ReadRepoCacheBinding returns the repo cache root cityRoot's packs were last
// materialized into, or "" when no binding has been recorded (a city installed
// before this record existed, or one that has never installed a remote import).
// An unreadable record is reported as absent rather than fatal: the binding is
// a diagnostic guard, and losing it must never make a city unloadable.
func ReadRepoCacheBinding(cityRoot string) string {
	data, err := os.ReadFile(RepoCacheBindingPath(cityRoot))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// WriteRepoCacheBinding records root as the cache the city's packs live in.
func WriteRepoCacheBinding(cityRoot, root string) error {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	path := RepoCacheBindingPath(cityRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating city runtime root: %w", err)
	}
	if err := os.WriteFile(path, []byte(root+"\n"), 0o644); err != nil {
		return fmt.Errorf("recording repo cache root: %w", err)
	}
	return nil
}

// RemoveRepoCacheBinding clears the recorded root so the next install re-records
// whichever root it materializes into.
func RemoveRepoCacheBinding(cityRoot string) error {
	if err := os.Remove(RepoCacheBindingPath(cityRoot)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clearing repo cache root record: %w", err)
	}
	return nil
}

// SameRepoCacheRoot compares two repo cache roots tolerantly: lexical cleaning
// first, then a best-effort symlink resolution so a root and a symlink to it
// are not reported as different caches. An empty root on either side compares
// equal, which is how callers disable the comparison when no root is known.
func SameRepoCacheRoot(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	ca, cb := filepath.Clean(a), filepath.Clean(b)
	if ca == cb {
		return true
	}
	ra, errA := filepath.EvalSymlinks(ca)
	rb, errB := filepath.EvalSymlinks(cb)
	if errA != nil || errB != nil {
		return false
	}
	return ra == rb
}
