package providerledger

import (
	"strings"
	"testing"
	"time"
)

func horizonTestEntries(expiries ...time.Time) []Entry {
	entries := make([]Entry, 0, len(expiries))
	for i, exp := range expiries {
		entries = append(entries, Entry{
			ID: string(rune('a' + i)),
			Claims: []ContractClaim{{
				Constructor: SymbolRef{ImportPath: "internal/runtime/x", Name: "New"},
				Contract:    ContractRuntimeProvider,
				Disposition: DispositionWaived,
				Waiver:      &Waiver{Owner: "ga-80po0c.3", Expires: exp, Reason: "because"},
			}},
		})
	}
	return entries
}

func TestWaiversExpiringWithin(t *testing.T) {
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)

	t.Run("inside the window is reported", func(t *testing.T) {
		got := WaiversExpiringWithin(horizonTestEntries(now.Add(3*24*time.Hour)), now, DefaultWaiverWarningWindow)
		if len(got) != 1 {
			t.Fatalf("got %d warnings, want 1", len(got))
		}
	})

	t.Run("outside the window is silent", func(t *testing.T) {
		got := WaiversExpiringWithin(horizonTestEntries(now.Add(30*24*time.Hour)), now, DefaultWaiverWarningWindow)
		if len(got) != 0 {
			t.Fatalf("got %d warnings, want 0", len(got))
		}
	})

	// An already-expired waiver is Validate's job and is already failing the
	// build loudly. Reporting it here too would make the advance warning fire
	// forever after an expiry, which is how a channel stops being read.
	t.Run("already expired is left to Validate", func(t *testing.T) {
		got := WaiversExpiringWithin(horizonTestEntries(now.Add(-time.Hour)), now, DefaultWaiverWarningWindow)
		if len(got) != 0 {
			t.Fatalf("got %d warnings for an EXPIRED waiver, want 0", len(got))
		}
	})

	t.Run("boundary: exactly at the window edge is not yet warned", func(t *testing.T) {
		got := WaiversExpiringWithin(horizonTestEntries(now.Add(DefaultWaiverWarningWindow)), now, DefaultWaiverWarningWindow)
		if len(got) != 0 {
			t.Fatalf("got %d warnings at the exact edge, want 0", len(got))
		}
	})

	t.Run("sorted by expiry for stable output", func(t *testing.T) {
		got := WaiversExpiringWithin(horizonTestEntries(
			now.Add(5*24*time.Hour), now.Add(1*24*time.Hour), now.Add(3*24*time.Hour),
		), now, DefaultWaiverWarningWindow)
		if len(got) != 3 {
			t.Fatalf("got %d warnings, want 3", len(got))
		}
		for i := 1; i < len(got); i++ {
			if got[i].Expires.Before(got[i-1].Expires) {
				t.Fatalf("warnings not sorted by expiry: %v", got)
			}
		}
	})
}

func TestFormatWaiverExpiryWarnings(t *testing.T) {
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)

	// Empty MUST render empty: the caller treats "" as "raise no notification".
	// A periodic all-clear is how a channel becomes unread, which is the exact
	// failure this mechanism exists to prevent.
	if got := FormatWaiverExpiryWarnings(nil, now, DefaultWaiverWarningWindow); got != "" {
		t.Fatalf("empty warnings rendered %q, want empty", got)
	}

	got := FormatWaiverExpiryWarnings(
		WaiversExpiringWithin(horizonTestEntries(now.Add(3*24*time.Hour)), now, DefaultWaiverWarningWindow), now, DefaultWaiverWarningWindow)
	for _, want := range []string{"expire within 14 days", "2026-08-27", "ga-80po0c.3", "EVERY pull", "Do not bump it bare"} {
		if !strings.Contains(got, want) {
			t.Fatalf("report missing %q:\n%s", want, got)
		}
	}
}

// A report that misstates its own window is believed, so pin it.
func TestFormatWaiverExpiryWarningsReportsTheWindowItWasGiven(t *testing.T) {
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	window := 60 * 24 * time.Hour
	got := FormatWaiverExpiryWarnings(
		WaiversExpiringWithin(horizonTestEntries(now.Add(30*24*time.Hour)), now, window), now, window)
	if !strings.Contains(got, "within 60 days") {
		t.Fatalf("header must report the window actually used, got:\n%s", got)
	}
	if strings.Contains(got, "within 14 days") {
		t.Fatalf("header reported the default window instead of the one passed:\n%s", got)
	}
}

// TestLiveCatalogWaiverHorizon is the one that would have caught 2026-08-12. It
// reports rather than fails: failing here would red every PR for the 14 days
// BEFORE an expiry, which is the same disease in an earlier costume.
func TestLiveCatalogWaiverHorizon(t *testing.T) {
	report := FormatWaiverExpiryWarnings(
		WaiversExpiringWithin(Catalog(), time.Now(), DefaultWaiverWarningWindow), time.Now(), DefaultWaiverWarningWindow)
	if report == "" {
		return
	}
	t.Log("\n" + report)
}
