package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/fsys"
)

// unreachableRigStoreRefusal is the refusal the bundled bd provider prints
// when init meets an existing store whose Dolt server does not answer
// (examples/bd/assets/scripts/gc-beads-bd.sh, op_init).
const unreachableRigStoreRefusal = "managed Dolt server unreachable while inspecting existing store"

// writeUnreachableRigStoreSpy is writeSpyScript with one change: init of the
// scope whose path ends in failingSuffix fails the way the bd provider does
// when that scope's Dolt server cannot be reached (exit 1; exit 2 would read
// as "not needed"). Every other op, and init of every other scope, succeeds.
// Init argv is "init <scopeDir> <prefix> [<doltDatabase>]".
func writeUnreachableRigStoreSpy(t *testing.T, logFile, failingSuffix string) string {
	t.Helper()
	return writeNamedTestScript(t, "spy-beads.sh", `#!/bin/sh
echo "$@" >> "`+logFile+`"
case "$1" in
  init)
    case "$2" in
      *`+failingSuffix+`)
        echo "`+unreachableRigStoreRefusal+` '${4:-$3}'; refusing to force-reinitialize (data-safety). retry once the Dolt server is reachable." >&2
        exit 1
        ;;
    esac
    mkdir -p "$2/.beads"
    ;;
esac
exit 0
`)
}

// TestPrepareCityForSupervisorStartsCityWhenOneRigStoreIsUnreachable pins the
// cold-start contract for a city whose rigs keep their bead stores on
// different servers. When one rig's store cannot be reached, the supervisor
// must still prepare the city: the city scope and every healthy rig are
// initialized, and the unreachable rig is reported by name and cause. Today
// the rig-init loop in startBeadsLifecycle returns on the first failure, so
// prepareCityForSupervisor fails the whole city and the supervisor retries it
// on backoff while every healthy rig stays dark.
//
// The suspended case is the operator's only lever today, `gc rig suspend` on
// the unreachable rig. The suspension is recorded, but the bead-store
// lifecycle never reads it, so the city still does not start.
//
// The control case proves the fixture: with every store reachable, the same
// city prepares cleanly.
func TestPrepareCityForSupervisorStartsCityWhenOneRigStoreIsUnreachable(t *testing.T) {
	cases := []struct {
		name              string
		remoteUnreachable bool
		suspendRemote     bool
	}{
		{name: "control_all_rig_stores_reachable"},
		{name: "one_rig_store_unreachable", remoteUnreachable: true},
		{name: "unreachable_rig_suspended_by_operator", remoteUnreachable: true, suspendRemote: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearGCEnv(t)
			cityPath := filepath.Join(t.TempDir(), "city")
			remotePath := filepath.Join(cityPath, "rigs", "remote")
			localPath := filepath.Join(cityPath, "rigs", "local")
			for _, dir := range []string{remotePath, localPath} {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			siteToml := "workspace_name = \"test-city\"\n\n" +
				"[[rig]]\nname = \"remote\"\npath = \"" + remotePath + "\"\n\n" +
				"[[rig]]\nname = \"local\"\npath = \"" + localPath + "\"\n"
			writeSchema2RigCity(t, cityPath, "test-city",
				"[workspace]\n\n[[rigs]]\nname = \"remote\"\n\n[[rigs]]\nname = \"local\"\n", siteToml)

			failingSuffix := "/no-such-scope"
			if tc.remoteUnreachable {
				failingSuffix = "/rigs/remote"
			}
			logFile := filepath.Join(t.TempDir(), "ops.log")
			t.Setenv("GC_BEADS", "exec:"+writeUnreachableRigStoreSpy(t, logFile, failingSuffix))
			// Registered after the GC_BEADS Setenv so its cleanup still sees
			// the spy provider.
			cleanupManagedDoltTestCity(t, cityPath)

			if tc.suspendRemote {
				var out, errOut bytes.Buffer
				if code := doRigSuspend(fsys.OSFS{}, cityPath, "remote", &out, &errOut); code != 0 {
					t.Fatalf("gc rig suspend remote: exit %d: %s", code, errOut.String())
				}
			}

			// The unreachable rig is declared first, so a loop that stops at
			// the first failure never reaches the healthy rig.
			cfg := config.DefaultCity("test-city")
			cfg.Rigs = []config.Rig{
				{Name: "remote", Path: remotePath},
				{Name: "local", Path: localPath},
			}
			var stderr bytes.Buffer
			err := prepareCityForSupervisor(cityPath, "test-city", &cfg, &stderr, nil)

			ops := readOpLog(t, logFile)
			initAttempted := func(dir string) bool {
				for _, op := range ops {
					if strings.HasPrefix(op, "init "+dir+" ") {
						return true
					}
				}
				return false
			}
			if !initAttempted(cityPath) {
				t.Fatalf("the fake provider never saw init of the city scope; ops=%v stderr=%q", ops, stderr.String())
			}
			if err != nil {
				t.Fatalf("prepareCityForSupervisor refused the whole city: %v\n"+
					"  healthy rig \"local\" initialized: %v\n"+
					"  provider ops: %v", err, initAttempted(localPath), ops)
			}
			if !initAttempted(localPath) {
				t.Fatalf("healthy rig \"local\" was never initialized; ops=%v", ops)
			}
			if tc.remoteUnreachable && !tc.suspendRemote {
				reported := false
				for _, line := range strings.Split(stderr.String(), "\n") {
					if strings.Contains(line, "remote") && strings.Contains(line, unreachableRigStoreRefusal) {
						reported = true
						break
					}
				}
				if !reported {
					t.Fatalf("stderr does not report rig \"remote\" with its cause %q:\n%s", unreachableRigStoreRefusal, stderr.String())
				}
			}
		})
	}
}
