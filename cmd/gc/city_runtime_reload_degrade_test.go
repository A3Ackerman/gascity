package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/events"
	"github.com/gastownhall/gascity/internal/runtime"
)

// twoRigCityTOML renders a city.toml declaring two rigs by name and one
// city-scoped named agent ("mayor" — no dir, so it is instantiated at city
// scope) whose provider env carries A=<aValue>. Rig paths live in
// .gc/site.toml (twoRigSiteTOML), the canonical v2 layout, so the config
// loader raises no legacy-surface warning: the only warning the reload may
// legitimately raise is the one about the rig whose bead lifecycle failed.
//
// [beads] provider = "file" matches the neighboring reload tests; the bead
// lifecycle itself resolves its provider through rawBeadsProvider, where the
// GC_BEADS env var takes precedence, which is how the exec spy is wired in.
func twoRigCityTOML(aValue string) []byte {
	return []byte(fmt.Sprintf(`[workspace]
name = "test-city"

[beads]
provider = "file"

[session]
provider = "fake"

[daemon]
shutdown_timeout = "1s"

[[rigs]]
name = "qcore"

[[rigs]]
name = "t3code"

[[agent]]
name = "mayor"

[agent.env]
A = %q
`, aValue))
}

// twoRigSiteTOML renders the machine-local .gc/site.toml binding the two rigs
// declared by twoRigCityTOML to their paths.
func twoRigSiteTOML(qcorePath, t3codePath string) []byte {
	return []byte(fmt.Sprintf("[[rig]]\nname = \"qcore\"\npath = %q\n\n[[rig]]\nname = \"t3code\"\npath = %q\n", qcorePath, t3codePath))
}

// mayorEnvA returns the value of the city-scoped mayor's env var A in cfg and
// whether the agent and the key were found.
func mayorEnvA(cfg *config.City) (string, bool) {
	if cfg == nil {
		return "", false
	}
	for _, a := range cfg.Agents {
		if a.Name != "mayor" {
			continue
		}
		v, ok := a.Env["A"]
		return v, ok
	}
	return "", false
}

// stubControllerRigStoreOpener swaps the controller's rig-store factory seam
// for in-memory stores, the way stubManagedDoltStoreOpeners does for the
// city and sweep stores, so the harness's own store opens — and the caching
// wrapper's background refresh — never reach the exec provider under test.
func stubControllerRigStoreOpener(t *testing.T) {
	t.Helper()
	prev := controllerStateOpenRigStoreAtForCity
	controllerStateOpenRigStoreAtForCity = func(context.Context, beads.StoreOpenOptions) (beads.StoreOpenResult, error) {
		return beads.StoreOpenResult{Store: beads.NewMemStore()}, nil
	}
	t.Cleanup(func() { controllerStateOpenRigStoreAtForCity = prev })
}

// TestCityRuntimeReloadAppliesCityScopedProviderEnvWhenOneRigBeadsInitFails
// pins the desired degrade behavior of config reload when exactly one rig's
// bead store cannot be initialized: a change that touches only a city-scoped
// agent's provider env — no rig store is involved — must still be applied to
// the city, with the failed rig reported by name as a warning rather than
// refusing the whole reload with "(keeping old config)".
//
// The failure is injected at the per-scope exec provider boundary (the spy
// from city_start_degrade_test.go, wired through GC_BEADS) so the REAL
// cityRuntimeStartBeadsLifecycle runs on the reload path: a per-rig degrade
// implemented inside startBeadsLifecycle is exercised, not bypassed. The
// provider error is the incident's shape: the bd provider's data-safety
// refusal for one rig while the city scope and the other rig are healthy.
func TestCityRuntimeReloadAppliesCityScopedProviderEnvWhenOneRigBeadsInitFails(t *testing.T) {
	clearGCEnv(t)
	cityPath := t.TempDir()
	tomlPath := filepath.Join(cityPath, "city.toml")
	qcorePath := filepath.Join(cityPath, "rigs", "qcore")
	t3codePath := filepath.Join(cityPath, "rigs", "t3code")
	for _, dir := range []string{qcorePath, t3codePath, filepath.Dir(config.SiteBindingPath(cityPath))} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", dir, err)
		}
	}
	logFile := filepath.Join(t.TempDir(), "beads-ops.log")
	t.Setenv("GC_BEADS", "exec:"+writeOneRigFailingBeadsSpy(t, logFile, qcorePath))
	// Registered after the GC_BEADS Setenv so its LIFO cleanup still sees the
	// spy provider and its "stop" op lands there.
	cleanupManagedDoltTestCity(t, cityPath)
	stubManagedDoltStoreOpeners(t)
	stubControllerRigStoreOpener(t)
	if err := os.WriteFile(config.SiteBindingPath(cityPath), twoRigSiteTOML(qcorePath, t3codePath), 0o644); err != nil {
		t.Fatalf("write site binding: %v", err)
	}
	if err := os.WriteFile(tomlPath, twoRigCityTOML("1"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(osFS{}, tomlPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	// config.Load skips the site-binding overlay that the reload path applies;
	// apply it here so the booted config carries the same rig paths.
	siteWarnings, err := config.ApplySiteBindings(osFS{}, cityPath, cfg)
	if err != nil {
		t.Fatalf("apply site bindings: %v", err)
	}
	if len(siteWarnings) != 0 {
		t.Fatalf("site binding warnings = %v, want none", siteWarnings)
	}
	for name, want := range map[string]string{"qcore": qcorePath, "t3code": t3codePath} {
		found := false
		for _, r := range cfg.Rigs {
			if r.Name == name {
				found = true
				if r.Path != want {
					t.Fatalf("precondition: rig %q path = %q, want %q", name, r.Path, want)
				}
			}
		}
		if !found {
			t.Fatalf("precondition: rig %q not declared", name)
		}
	}
	if got, ok := mayorEnvA(cfg); !ok || got != "1" {
		t.Fatalf("precondition: mayor env A = %q (present=%v), want \"1\"", got, ok)
	}
	sp := runtime.NewFake()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cr := newTestCityRuntime(t, CityRuntimeParams{
		CityPath:  cityPath,
		CityName:  "test-city",
		TomlPath:  tomlPath,
		LogPrefix: "gc reload",
		Cfg:       cfg,
		SP:        sp,
		BuildFn: func(*config.City, runtime.Provider, beads.Store) DesiredStateResult {
			return DesiredStateResult{State: map[string]TemplateParams{}}
		},
		Dops:   newDrainOps(sp),
		Rec:    events.Discard,
		Stdout: &stdout,
		Stderr: &stderr,
	})

	cs := newControllerState(context.Background(), cfg, sp, events.NewFake(), "test-city", cityPath)
	cs.cityBeadStore = beads.NewMemStore()
	cr.setControllerState(cs)
	cr.sessionDrains = newDrainTracker()

	oldRev := cr.configRev

	// The only change: the mayor's provider env A goes from "1" to "2".
	if err := os.WriteFile(tomlPath, twoRigCityTOML("2"), 0o644); err != nil {
		t.Fatalf("write updated config: %v", err)
	}
	lastProviderName := "fake"
	reply := cr.reloadConfigTraced(context.Background(), &lastProviderName, cityPath, nil, reloadSourceManual)

	ops := readSpyOps(t, logFile)
	scopes := recordedInitScopes(ops)
	if !initScopeRecorded(scopes, cityPath) {
		t.Fatalf("the exec provider was never asked to init the city scope %q on the reload path; ops=%v reply=%+v stderr=%q", cityPath, spyOpSummaries(ops), reply, stderr.String())
	}
	if reply.Outcome == reloadOutcomeFailed {
		got, _ := mayorEnvA(cr.cfg)
		t.Fatalf(`reload refused for the whole city because rig "qcore" beads init failed: outcome=%q err=%q; the mayor's provider env (A=%q) was not applied although it touches no rig store; spy init scopes=%v`,
			reply.Outcome, reply.Error, got, scopes)
	}
	if reply.Outcome != reloadOutcomeApplied {
		t.Fatalf("reply.Outcome = %q, want %q", reply.Outcome, reloadOutcomeApplied)
	}
	if reply.Error != "" {
		t.Fatalf("reply.Error = %q, want empty", reply.Error)
	}
	if !warningsContain(reply.Warnings, "qcore") {
		t.Fatalf("reply.Warnings = %v, want the failed rig reported by name (qcore)", reply.Warnings)
	}
	t.Logf("provider cause %q present: warnings=%v stderr=%v; spy init scopes=%v",
		managedDoltUnreachableFragment, warningsContain(reply.Warnings, managedDoltUnreachableFragment), strings.Contains(stderr.String(), managedDoltUnreachableFragment), scopes)
	if got, ok := mayorEnvA(cr.cfg); !ok || got != "2" {
		t.Fatalf("mayor env A after reload = %q (present=%v), want \"2\"", got, ok)
	}
	if cr.configRev == oldRev {
		t.Fatalf("configRev = %q, want new revision", cr.configRev)
	}
	if lastProviderName != "fake" {
		t.Fatalf("lastProviderName = %q, want fake", lastProviderName)
	}
	if !strings.Contains(stdout.String(), "Config reloaded:") {
		t.Fatalf("stdout = %q, want reload success message", stdout.String())
	}
	if strings.Contains(stderr.String(), "keeping old config") {
		t.Fatalf("stderr = %q, want no whole-city reload refusal", stderr.String())
	}
}
