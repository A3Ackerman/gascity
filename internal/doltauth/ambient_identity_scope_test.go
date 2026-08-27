package doltauth

import "testing"

// The regression these guard is ga-3qvmjj: after the 2026-08-27 qcore hub flip,
// an agent session projected for the hub could not open the LOCAL city store,
// because the endpoint resolved per store while the credential resolved per
// process. hq is the coordination plane, so the agent lost mail, ga- beads, its
// hook queue and its work queue at once.

func TestResolveUser_HubIdentityDoesNotLeakOntoLocalStore(t *testing.T) {
	// Exactly the live shape read off agent qcore/archer's pane.
	t.Setenv("GC_DOLT_HOST", "100.71.23.94")
	t.Setenv("GC_DOLT_PORT", "3307")
	t.Setenv("GC_DOLT_USER", "cherub")

	got := resolveUser("", "127.0.0.1", 51361)
	if got == "cherub" {
		t.Fatal("hub identity was applied to the local managed store — this is the 1045 that took hq down for flip-primed sessions")
	}
	if got != "" {
		t.Fatalf("resolveUser = %q, want the fallback (empty), not the mismatched ambient override", got)
	}
}

func TestResolveUser_AppliesToItsOwnEndpoint(t *testing.T) {
	t.Setenv("GC_DOLT_HOST", "100.71.23.94")
	t.Setenv("GC_DOLT_PORT", "3307")
	t.Setenv("GC_DOLT_USER", "cherub")

	if got := resolveUser("", "100.71.23.94", 3307); got != "cherub" {
		t.Fatalf("resolveUser = %q, want cherub — the override must still apply to the endpoint it was set for", got)
	}
}

// The operator override is documented behaviour and must survive: doltauth
// reads GC_DOLT_USER via os.Getenv precisely so an operator can force it.
func TestResolveUser_BareOverrideStillAppliesEverywhere(t *testing.T) {
	t.Setenv("GC_DOLT_HOST", "")
	t.Setenv("GC_DOLT_PORT", "")
	t.Setenv("GC_DOLT_USER", "operator")

	for _, tc := range []struct {
		host string
		port int
	}{{"127.0.0.1", 51361}, {"100.71.23.94", 3307}, {"", 0}} {
		if got := resolveUser("fallback", tc.host, tc.port); got != "operator" {
			t.Fatalf("resolveUser(%q,%d) = %q, want operator — a bare override names no endpoint, so nothing contradicts it", tc.host, tc.port, got)
		}
	}
}

// Narrow only where the mismatch is provable: an unknown target endpoint is not
// evidence of a mismatch.
func TestResolveUser_UnknownTargetKeepsOverride(t *testing.T) {
	t.Setenv("GC_DOLT_HOST", "100.71.23.94")
	t.Setenv("GC_DOLT_PORT", "3307")
	t.Setenv("GC_DOLT_USER", "cherub")

	if got := resolveUser("fallback", "", 0); got != "cherub" {
		t.Fatalf("resolveUser = %q, want cherub — an unknown target cannot contradict the override", got)
	}
}

func TestResolveUser_NoOverrideUsesFallback(t *testing.T) {
	t.Setenv("GC_DOLT_USER", "")
	if got := resolveUser("target-user", "127.0.0.1", 51361); got != "target-user" {
		t.Fatalf("resolveUser = %q, want target-user", got)
	}
}

// localhost and 127.0.0.1 are the same server and both spellings appear in the
// config surface; treating them as different would decline a valid override.
func TestResolveUser_LocalhostAndLoopbackAreTheSameEndpoint(t *testing.T) {
	t.Setenv("GC_DOLT_HOST", "localhost")
	t.Setenv("GC_DOLT_PORT", "51361")
	t.Setenv("GC_DOLT_USER", "operator")

	if got := resolveUser("", "127.0.0.1", 51361); got != "operator" {
		t.Fatalf("resolveUser = %q, want operator — localhost and 127.0.0.1 are one endpoint", got)
	}
}

// A port-only mismatch is enough: same host, different server.
func TestResolveUser_PortOnlyMismatchDeclines(t *testing.T) {
	t.Setenv("GC_DOLT_HOST", "127.0.0.1")
	t.Setenv("GC_DOLT_PORT", "3307")
	t.Setenv("GC_DOLT_USER", "cherub")

	if got := resolveUser("fallback", "127.0.0.1", 51361); got != "fallback" {
		t.Fatalf("resolveUser = %q, want fallback — same host but a different server is still a different endpoint", got)
	}
}
