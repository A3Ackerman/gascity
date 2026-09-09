package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/config"
)

// managedDoltUnreachableFragment is the stable prefix of the bd provider's
// data-safety refusal (examples/bd/assets/scripts/gc-beads-bd.sh, op_init):
// the message a rig's init prints when the managed Dolt server cannot be
// reached while an existing store is being inspected.
const managedDoltUnreachableFragment = "managed Dolt server unreachable while inspecting existing store"

// The spy helpers below are shared with city_runtime_reload_degrade_test.go:
// both degrade tests inject the same provider failure at the per-scope exec
// boundary so a fix inside startBeadsLifecycle is exercised by both.

// writeOneRigFailingBeadsSpy writes an exec bead-provider script that records
// every op to logFile as its tab-separated argv (one op per line, so scope
// paths with spaces survive), answers every op with success, and fails
// "init" for exactly one scope directory with the provider's own refusal on
// stderr and exit status 1 — status 2 would be read as "not needed" by
// runProviderOpWithEnv. The init argv contract is
// "init <scopeDir> <prefix> [<doltDatabase>]" (initBeadsForDirWithExecutor),
// so the scope is $2 and the store name reported is $4, falling back to $3.
//
// The script is deliberately NOT named gc-beads-bd.sh: that basename makes
// contract.ProviderUsesBDContract and execProviderUsesCanonicalBdScopeFiles
// classify it as the managed-Dolt provider and routes init through port,
// publication and scope-readiness machinery that is not under test here.
func writeOneRigFailingBeadsSpy(t *testing.T, logFile, failingScope string) string {
	t.Helper()
	content := `#!/bin/sh
{ printf '%s\t' "$@"; echo; } >> "` + logFile + `"
case "$1" in
  init)
    if [ "$2" = "` + failingScope + `" ]; then
      echo "` + managedDoltUnreachableFragment + ` '${4:-$3}'; refusing to force-reinitialize (data-safety). retry once the Dolt server is reachable." >&2
      exit 1
    fi
    mkdir -p "$2/.beads"
    ;;
esac
exit 0
`
	return writeNamedTestScript(t, "spy-beads.sh", content)
}

// readSpyOps returns the ops the spy logged, each as its argv fields; a
// missing log means the provider was never invoked.
func readSpyOps(t *testing.T, logFile string) [][]string {
	t.Helper()
	data, err := os.ReadFile(logFile)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatalf("read spy log: %v", err)
	}
	var ops [][]string
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if n := len(fields); n > 0 && fields[n-1] == "" {
			fields = fields[:n-1] // the delimiter after the last argument
		}
		if len(fields) > 0 {
			ops = append(ops, fields)
		}
	}
	return ops
}

// spyOpSummaries renders logged ops for failure messages.
func spyOpSummaries(ops [][]string) []string {
	out := make([]string, 0, len(ops))
	for _, op := range ops {
		out = append(out, strings.Join(op, " "))
	}
	return out
}

// recordedInitScopes extracts, in order, the scope directory of every
// "init" op the spy logged.
func recordedInitScopes(ops [][]string) []string {
	var scopes []string
	for _, op := range ops {
		if len(op) >= 2 && op[0] == "init" {
			scopes = append(scopes, op[1])
		}
	}
	return scopes
}

// initScopeRecorded reports whether the spy was asked to init dir.
func initScopeRecorded(scopes []string, dir string) bool {
	for _, scope := range scopes {
		if scope == dir {
			return true
		}
	}
	return false
}

// stderrLineNames reports whether out carries a line naming rig — the
// degraded-rig report the supervisor should print instead of refusing the
// whole city.
func stderrLineNames(out, rig string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, rig) {
			return true
		}
	}
	return false
}

// TestPrepareCityForSupervisorPreparesCityWhenOneRigBeadsInitFails is the
// start-path twin of the reload degrade test: when exactly one rig's bead
// store cannot be initialized, prepareCityForSupervisor must still succeed
// — its caller publishes the city only on nil, so today no session of any
// scope starts — with the city scope and every healthy rig initialized and
// the failed rig reported by name as degraded, rather than returning
// `beads lifecycle: init rig "qcore" beads: ...` for the whole city.
//
// The provider failure is the exact shape of the incident: the bd
// provider's data-safety refusal for one rig while the city scope and the
// other rig's local stores are healthy.
func TestPrepareCityForSupervisorPreparesCityWhenOneRigBeadsInitFails(t *testing.T) {
	clearGCEnv(t)
	cityPath := filepath.Join(t.TempDir(), "city")
	qcorePath := filepath.Join(cityPath, "rigs", "qcore")
	t3codePath := filepath.Join(cityPath, "rigs", "t3code")
	for _, dir := range []string{qcorePath, t3codePath} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", dir, err)
		}
	}
	logFile := filepath.Join(t.TempDir(), "beads-ops.log")
	t.Setenv("GC_BEADS", "exec:"+writeOneRigFailingBeadsSpy(t, logFile, qcorePath))
	// Registered after the GC_BEADS Setenv so its LIFO cleanup still sees the
	// spy provider and its "stop" op lands there instead of on a missing
	// gc-beads-bd.sh shim.
	cleanupManagedDoltTestCity(t, cityPath)

	// DefaultCity carries the city-scoped "mayor" agent: the session that
	// never starts on stock although it touches no rig store.
	cfg := config.DefaultCity("test-city")
	cfg.Rigs = []config.Rig{
		{Name: "qcore", Path: qcorePath},
		{Name: "t3code", Path: t3codePath},
	}

	var stderr bytes.Buffer
	var progress []string
	err := prepareCityForSupervisor(cityPath, "test-city", &cfg, &stderr, func(status string) {
		progress = append(progress, status)
	})

	ops := readSpyOps(t, logFile)
	scopes := recordedInitScopes(ops)
	if !initScopeRecorded(scopes, cityPath) {
		t.Fatalf("the fake bead provider was never asked to init the city scope %q; ops=%v progress=%v stderr=%q", cityPath, spyOpSummaries(ops), progress, stderr.String())
	}
	if err != nil {
		t.Fatalf(`city start refused because rig "qcore" beads init failed: err=%q; recorded init scopes=%v (city initialized=%v, t3code initialized=%v); the city scope and rig "t3code" are never published although their stores are healthy`,
			err, scopes, initScopeRecorded(scopes, cityPath), initScopeRecorded(scopes, t3codePath))
	}
	if !initScopeRecorded(scopes, t3codePath) {
		t.Fatalf(`healthy rig "t3code" (%q) was not initialized; scopes=%v`, t3codePath, scopes)
	}
	if !initScopeRecorded(scopes, qcorePath) {
		t.Fatalf(`rig "qcore" (%q) init was never attempted; scopes=%v`, qcorePath, scopes)
	}
	if !stderrLineNames(stderr.String(), "qcore") {
		t.Fatalf("stderr = %q, want rig \"qcore\" reported as degraded by name", stderr.String())
	}
	t.Logf("provider cause %q present in stderr: %v", managedDoltUnreachableFragment, strings.Contains(stderr.String(), managedDoltUnreachableFragment))
}
