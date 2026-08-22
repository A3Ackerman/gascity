package packman

import (
	"errors"
	"fmt"

	"github.com/gastownhall/gascity/internal/config"
)

// ErrCacheRootMismatch reports that a city's packs were installed into a
// different shared repo cache root than the one the current process resolves.
var ErrCacheRootMismatch = errors.New("this city's packs are installed in a different repo cache root")

// CacheRootMismatchError names both roots so the operator can see, in one line,
// which side moved. The overwhelmingly common cause is an unexported GC_HOME:
// the city is operated under an explicit GC_HOME (from a login profile, a
// service unit, a supervisor env) while some other shell — an agent's shell,
// `ssh host '<cmd>'`, a cron entry — has it unset and silently falls back to
// $HOME/.gc.
type CacheRootMismatchError struct {
	CityRoot   string
	BoundRoot  string
	ActiveRoot string
}

func (e *CacheRootMismatchError) Error() string {
	return fmt.Sprintf(
		"%s: packs for %s are installed under %s but this process resolves %s; "+
			"set GC_HOME to the recorded root and retry, or pass --rebind-cache-root to move the city onto the active root",
		ErrCacheRootMismatch, e.CityRoot, e.BoundRoot, e.ActiveRoot)
}

func (e *CacheRootMismatchError) Unwrap() error { return ErrCacheRootMismatch }

// activeRepoCacheRoot returns the repo cache root this process resolves, or ""
// when none is resolvable (a hermetic test binary with no GC_HOME). A "" root
// disables the binding checks rather than failing them, so behavior is
// unchanged wherever a root cannot be determined.
func activeRepoCacheRoot() string {
	root, err := RepoCacheRoot()
	if err != nil {
		return ""
	}
	return root
}

// CheckCacheRootBinding fails when cityRoot's packs were materialized into a
// repo cache root other than the one this process would install into.
//
// It is the guard that turns a silent "locked but not cached" wedge into an
// immediate, self-explanatory refusal. packs.lock is city state — durable,
// shared, committed — while the repo cache root is process state derived from
// the environment. Without the guard, a run whose environment resolves a
// different root advances the city's pins while the clones land somewhere the
// city is never served from, and every later config load fails demanding the
// very install command that just reported success.
func CheckCacheRootBinding(cityRoot string) error {
	bound := config.ReadRepoCacheBinding(cityRoot)
	if bound == "" {
		return nil
	}
	active := activeRepoCacheRoot()
	if config.SameRepoCacheRoot(bound, active) {
		return nil
	}
	return &CacheRootMismatchError{CityRoot: cityRoot, BoundRoot: bound, ActiveRoot: active}
}

// stampCacheRootBinding records the root this process just materialized the
// city's packs into. Callers invoke it only after the materialization succeeded,
// so the record always describes clones that actually exist.
func stampCacheRootBinding(cityRoot string) error {
	return config.WriteRepoCacheBinding(cityRoot, activeRepoCacheRoot())
}

// RebindCacheRoot clears cityRoot's recorded repo cache root so the next install
// records whichever root it materializes into. This is the deliberate "I moved
// GC_HOME and I mean it" escape from the binding check; the next install
// refetches whatever the new root is missing.
func RebindCacheRoot(cityRoot string) error {
	return config.RemoveRepoCacheBinding(cityRoot)
}
