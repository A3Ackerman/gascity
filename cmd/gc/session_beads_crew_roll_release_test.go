package main

import (
	"bytes"
	"testing"

	"github.com/gastownhall/gascity/internal/beadmeta"
	"github.com/gastownhall/gascity/internal/beads"
	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/session"
)

// ga-sdynmb: `gc session close <agent>` is a routine move for a live agent — a
// provider flip or config-drift roll closes the session so the supervisor
// respawns it under the same configured identity — and cmdSessionClose hands
// the closing session's whole backlog to unclaimWorkAssignedToRetiredSessionBead
// with fallbackRoute="". That clears the assignee on every open/in_progress
// bead the agent held, across the city AND rig stores, and — the fallback being
// empty — stamps no gc.run_target either. The released bead is then invisible
// to every recovery lane this codebase has: namedWorkReady keys direct named
// demand on the bead's Assignee (build_desired_state.go — and deliberately NOT
// on raw gc.routed_to), the pool demand probe keys on gc.routed_to,
// releaseOrphanedPoolAssignmentsWhenSnapshotsComplete skips empty-routed beads,
// and witness orphan recovery is scoped to POOL/EPHEMERAL identities so
// named-session work is never dumped into the pool. On one deployed city that
// left 91 beads across eight crew agents open+unassigned+unrouted over three
// roll windows, and manual repair did not survive the next roll. A restart is
// not a retirement.
//
// These tests are the falsifiable floor: crewRollConfig's named session is
// STILL in the config, so the identity outlives the close, and each "Keeps"
// assertion below fails on unpatched source (assignee comes back "").
//
// NAMESPACE NOTE, and the reason the fixtures look redundant: an agent lives
// under TWO address forms — the CONFIG form carried in the session bead's
// "template" metadata (here "qcore/cherub-law.ray") and the RUNTIME form that
// is its actual assignee ("qcore/ray"). Only the runtime form is what the
// agent's own hook and namedWorkReady match on, so every assertion here is
// written against the RUNTIME form. Asserting the config form instead is the
// known false pass this bead called out.

const (
	crewRuntimeIdentity = "qcore/ray"            // assignee; what ray's own hook matches
	crewConfigTemplate  = "qcore/cherub-law.ray" // session bead "template" metadata
	crewSessionName     = "qcore--ray"           // ephemeral session_name form
)

func intPtrCrewRoll(n int) *int { return &n }

// crewRollConfig is a city whose [[named_session]] qcore/ray is backed by a
// live (unsuspended) agent — i.e. the agent survives the roll.
func crewRollConfig(suspended bool) *config.City {
	// Deliberately the PRODUCTION shape, not the convenient one: a real
	// [[named_session]] declares name="ray" alongside template="cherub-law.ray",
	// so the agent template ("qcore/cherub-law.ray") and the session identity
	// ("qcore/ray") are DIFFERENT strings — QualifiedName is Dir+"/"+IdentityName
	// and IdentityName prefers Name over Template. Collapsing the two here would
	// let a guard that keys on the template form pass this suite and still be a
	// no-op against the live city.
	return &config.City{
		Agents: []config.Agent{
			{Name: "cherub-law.ray", Dir: "qcore", Suspended: suspended, MaxActiveSessions: intPtrCrewRoll(1)},
		},
		NamedSessions: []config.NamedSession{
			{Name: "ray", Template: "cherub-law.ray", Dir: "qcore", Mode: "always"},
		},
	}
}

// crewSessionBead is the closing session bead for the named crew agent, shaped
// like the real ones: template metadata in the CONFIG form, identity in the
// RUNTIME form.
func crewSessionBead() beads.Bead {
	return beads.Bead{
		ID:     "ga-oldsession",
		Type:   sessionBeadType,
		Status: "open",
		Labels: []string{sessionBeadLabel},
		Metadata: map[string]string{
			"configured_named_session":  "true",
			"configured_named_identity": crewRuntimeIdentity,
			"agent_name":                crewRuntimeIdentity,
			"alias":                     crewRuntimeIdentity,
			"session_name":              crewSessionName,
			"template":                  crewConfigTemplate,
			"state":                     "active",
		},
	}
}

// assertNotStranded fails with the exact defect signature from the bead:
// open + no assignee + no gc.routed_to + no gc.run_target is the triple that
// no subsystem will ever pick up.
func assertNotStranded(t *testing.T, got beads.Bead, what string) {
	t.Helper()
	if got.Assignee == "" &&
		got.Metadata[beadmeta.RoutedToMetadataKey] == "" &&
		got.Metadata[beadmeta.RunTargetMetadataKey] == "" {
		t.Fatalf("%s (%s) is STRANDED: open+unassigned+unrouted — invisible to namedWorkReady, the pool demand probe, and orphan recovery", what, got.ID)
	}
}

// TestSessionCloseKeepsConfiguredCrewWorkAcrossRoll is the core regression: a
// named crew agent that is still configured keeps BOTH an open and an
// in_progress bead, in the rig store, across the close.
func TestSessionCloseKeepsConfiguredCrewWorkAcrossRoll(t *testing.T) {
	cityStore := beads.NewMemStore()
	rigStore := beads.NewMemStore()

	openWork, err := rigStore.Create(beads.Bead{
		Title: "open crew work", Status: "open", Assignee: crewRuntimeIdentity,
	})
	if err != nil {
		t.Fatalf("create open work: %v", err)
	}
	inProgressWork, err := rigStore.Create(beads.Bead{
		Title: "in-flight crew epic", Assignee: crewRuntimeIdentity,
	})
	if err != nil {
		t.Fatalf("create in_progress work: %v", err)
	}
	// MemStore.Create forces Status="open", so drive the bead to in_progress
	// explicitly — otherwise this case silently degrades into a second copy of
	// the open-work case and stops covering the status-reset half of the bug.
	inProgress := "in_progress"
	if err := rigStore.Update(inProgressWork.ID, beads.UpdateOpts{Status: &inProgress}); err != nil {
		t.Fatalf("set in_progress: %v", err)
	}

	var stderr bytes.Buffer
	unclaimWorkAssignedToRetiredSessionBead(
		"", // no registered runtime, no CLI bindings: the plan is work leg + rig legs
		crewRollConfig(false),
		cityStore,
		map[string]beads.Store{"qcore": rigStore},
		crewSessionBead(),
		"", // the close path's fallbackRoute, verbatim from cmdSessionClose
		&stderr,
	)

	gotOpen, err := rigStore.Get(openWork.ID)
	if err != nil {
		t.Fatalf("get open work: %v", err)
	}
	if gotOpen.Assignee != crewRuntimeIdentity {
		t.Fatalf("open work Assignee = %q, want %q retained — a restart is not a retirement", gotOpen.Assignee, crewRuntimeIdentity)
	}
	assertNotStranded(t, gotOpen, "open crew work")

	gotInProgress, err := rigStore.Get(inProgressWork.ID)
	if err != nil {
		t.Fatalf("get in_progress work: %v", err)
	}
	if gotInProgress.Assignee != crewRuntimeIdentity {
		t.Fatalf("in_progress work Assignee = %q, want %q retained", gotInProgress.Assignee, crewRuntimeIdentity)
	}
	if gotInProgress.Status != "in_progress" {
		t.Fatalf("in_progress work Status = %q, want %q — a roll must not silently reset an in-flight epic to open", gotInProgress.Status, "in_progress")
	}
	assertNotStranded(t, gotInProgress, "in_progress crew work")
}

// TestSessionCloseKeepsConfiguredCrewWorkInCityStore covers the city-scoped
// half of the same sweep: a city-level named agent's own beads.
func TestSessionCloseKeepsConfiguredCrewWorkInCityStore(t *testing.T) {
	cityStore := beads.NewMemStore()
	work, err := cityStore.Create(beads.Bead{
		Title: "city crew work", Status: "open", Assignee: crewRuntimeIdentity,
	})
	if err != nil {
		t.Fatalf("create work: %v", err)
	}

	var stderr bytes.Buffer
	unclaimWorkAssignedToRetiredSessionBead(
		"", crewRollConfig(false), cityStore, nil, crewSessionBead(), "", &stderr,
	)

	got, err := cityStore.Get(work.ID)
	if err != nil {
		t.Fatalf("get work: %v", err)
	}
	if got.Assignee != crewRuntimeIdentity {
		t.Fatalf("city work Assignee = %q, want %q retained", got.Assignee, crewRuntimeIdentity)
	}
}

// TestSessionCloseStillReleasesRetiredNamedSessionWork is the companion that
// keeps the guard honest: an identity that is NOT in the config is genuinely
// retired, so its work must still be released.
func TestSessionCloseStillReleasesRetiredNamedSessionWork(t *testing.T) {
	cityStore := beads.NewMemStore()
	rigStore := beads.NewMemStore()
	work, err := rigStore.Create(beads.Bead{
		Title: "retired agent work", Assignee: crewRuntimeIdentity,
	})
	if err != nil {
		t.Fatalf("create work: %v", err)
	}
	inProgress := "in_progress"
	if err := rigStore.Update(work.ID, beads.UpdateOpts{Status: &inProgress}); err != nil {
		t.Fatalf("set in_progress: %v", err)
	}

	var stderr bytes.Buffer
	unclaimWorkAssignedToRetiredSessionBead(
		"",
		&config.City{}, // qcore/ray is no longer configured
		cityStore,
		map[string]beads.Store{"qcore": rigStore},
		crewSessionBead(),
		"qcore/fallback-route",
		&stderr,
	)

	got, err := rigStore.Get(work.ID)
	if err != nil {
		t.Fatalf("get work: %v", err)
	}
	if got.Assignee != "" {
		t.Fatalf("retired-agent work Assignee = %q, want cleared — a real retirement must still release its work", got.Assignee)
	}
	if got.Status != "open" {
		t.Fatalf("retired-agent work Status = %q, want %q", got.Status, "open")
	}
	if got.Metadata[beadmeta.RunTargetMetadataKey] != "qcore/fallback-route" {
		t.Fatalf("retired-agent work run_target = %q, want the fallback route stamped", got.Metadata[beadmeta.RunTargetMetadataKey])
	}
}

// TestSessionCloseStillReleasesSuspendedAgentWork pins the suspension carve-out
// that isConfiguredNamedSessionIdentity already encodes: a suspended agent's
// tier never claims, so keeping its assignee would orphan the bead with neither
// side picking it up.
func TestSessionCloseStillReleasesSuspendedAgentWork(t *testing.T) {
	cityStore := beads.NewMemStore()
	rigStore := beads.NewMemStore()
	work, err := rigStore.Create(beads.Bead{
		Title: "suspended agent work", Status: "open", Assignee: crewRuntimeIdentity,
	})
	if err != nil {
		t.Fatalf("create work: %v", err)
	}

	var stderr bytes.Buffer
	unclaimWorkAssignedToRetiredSessionBead(
		"",
		crewRollConfig(true), // backing agent suspended
		cityStore,
		map[string]beads.Store{"qcore": rigStore},
		crewSessionBead(),
		"qcore/fallback-route",
		&stderr,
	)

	got, err := rigStore.Get(work.ID)
	if err != nil {
		t.Fatalf("get work: %v", err)
	}
	if got.Assignee != "" {
		t.Fatalf("suspended-agent work Assignee = %q, want cleared", got.Assignee)
	}
}

// TestSessionCloseStillReleasesEphemeralIdentifierWork pins the other edge: even
// for a still-configured crew agent, work pinned to an identifier that DIES with
// this session (the session bead ID, the "rig--agent" session_name form) must
// still be released, or it strands on an address nothing will ever answer to.
func TestSessionCloseStillReleasesEphemeralIdentifierWork(t *testing.T) {
	cityStore := beads.NewMemStore()
	rigStore := beads.NewMemStore()

	sessionBead := crewSessionBead()
	byBeadID, err := rigStore.Create(beads.Bead{
		Title: "work pinned to the dying session bead", Status: "open", Assignee: sessionBead.ID,
	})
	if err != nil {
		t.Fatalf("create bead-ID work: %v", err)
	}
	bySessionName, err := rigStore.Create(beads.Bead{
		Title: "work pinned to the session_name form", Status: "open", Assignee: crewSessionName,
	})
	if err != nil {
		t.Fatalf("create session-name work: %v", err)
	}

	var stderr bytes.Buffer
	unclaimWorkAssignedToRetiredSessionBead(
		"",
		crewRollConfig(false),
		cityStore,
		map[string]beads.Store{"qcore": rigStore},
		sessionBead,
		"qcore/fallback-route",
		&stderr,
	)

	gotByID, err := rigStore.Get(byBeadID.ID)
	if err != nil {
		t.Fatalf("get bead-ID work: %v", err)
	}
	if gotByID.Assignee != "" {
		t.Fatalf("bead-ID work Assignee = %q, want cleared — that identifier dies with the session", gotByID.Assignee)
	}
	assertNotStranded(t, gotByID, "bead-ID work")

	gotByName, err := rigStore.Get(bySessionName.ID)
	if err != nil {
		t.Fatalf("get session-name work: %v", err)
	}
	if gotByName.Assignee != "" {
		t.Fatalf("session-name work Assignee = %q, want cleared", gotByName.Assignee)
	}
	assertNotStranded(t, gotByName, "session-name work")
}

// crewSessionInfo projects crewSessionBead through the session store the way
// the reconciler's stranded-repair lane reads it, keeping the Info-form tests
// on the same fixture the raw-form tests pin.
func crewSessionInfo(t *testing.T, store beads.Store) session.Info {
	t.Helper()
	created, err := store.Create(crewSessionBead())
	if err != nil {
		t.Fatalf("create session bead: %v", err)
	}
	info, err := session.NewStore(beads.SessionStore{Store: store}).Get(created.ID)
	if err != nil {
		t.Fatalf("projecting session info: %v", err)
	}
	return info
}

// TestStrandedRepairKeepsConfiguredNamedIdentityWork covers the Info form the
// stranded-pool-worker repair reuses: the guard must hold there too, both to
// keep the two forms byte-identical and because rerouting a configured named
// session's work into the pool queue via fallbackRoute is exactly the demand
// leak isConfiguredNamedSessionIdentity closed on the pool side (ga-i1d0tr
// Candidate B). Skipped work is neither Released nor Failed — its recovery
// lane is the named tier, so the repair is still clean.
func TestStrandedRepairKeepsConfiguredNamedIdentityWork(t *testing.T) {
	cityStore := beads.NewMemStore()
	rigStore := beads.NewMemStore()
	info := crewSessionInfo(t, cityStore)

	work, err := rigStore.Create(beads.Bead{
		Title: "named crew work under a stranded instance", Status: "open", Assignee: crewRuntimeIdentity,
	})
	if err != nil {
		t.Fatalf("create work: %v", err)
	}

	var stderr bytes.Buffer
	res := unclaimWorkAssignedToRetiredSessionInfo(
		"",
		crewRollConfig(false),
		cityStore,
		map[string]beads.Store{"qcore": rigStore},
		info,
		"qcore/pool-route",
		&stderr,
	)

	got, err := rigStore.Get(work.ID)
	if err != nil {
		t.Fatalf("get work: %v", err)
	}
	if got.Assignee != crewRuntimeIdentity {
		t.Fatalf("named-identity work Assignee = %q, want %q retained on the repair path too", got.Assignee, crewRuntimeIdentity)
	}
	if got.Metadata[beadmeta.RunTargetMetadataKey] != "" {
		t.Fatalf("named-identity work run_target = %q, want no pool reroute", got.Metadata[beadmeta.RunTargetMetadataKey])
	}
	if res.Failed != 0 {
		t.Fatalf("res.Failed = %d, want 0 — kept named work is not a failed release", res.Failed)
	}
	if res.Released != 0 {
		t.Fatalf("res.Released = %d, want 0 — nothing was detached", res.Released)
	}
}

// TestStrandedRepairStillReleasesUnconfiguredIdentityWork is the Info-form
// control: with the named session gone from config, the same sweep must detach
// and reroute exactly as before the guard.
func TestStrandedRepairStillReleasesUnconfiguredIdentityWork(t *testing.T) {
	cityStore := beads.NewMemStore()
	rigStore := beads.NewMemStore()
	info := crewSessionInfo(t, cityStore)

	work, err := rigStore.Create(beads.Bead{
		Title: "genuinely orphaned work", Status: "open", Assignee: crewRuntimeIdentity,
	})
	if err != nil {
		t.Fatalf("create work: %v", err)
	}

	var stderr bytes.Buffer
	res := unclaimWorkAssignedToRetiredSessionInfo(
		"",
		&config.City{}, // qcore/ray is no longer configured
		cityStore,
		map[string]beads.Store{"qcore": rigStore},
		info,
		"qcore/pool-route",
		&stderr,
	)

	got, err := rigStore.Get(work.ID)
	if err != nil {
		t.Fatalf("get work: %v", err)
	}
	if got.Assignee != "" {
		t.Fatalf("orphaned work Assignee = %q, want cleared", got.Assignee)
	}
	if got.Metadata[beadmeta.RunTargetMetadataKey] != "qcore/pool-route" {
		t.Fatalf("orphaned work run_target = %q, want the fallback route stamped", got.Metadata[beadmeta.RunTargetMetadataKey])
	}
	if res.Released != 1 {
		t.Fatalf("res.Released = %d, want 1", res.Released)
	}
}
