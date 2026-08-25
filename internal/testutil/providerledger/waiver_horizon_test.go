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

	// WaiversExpiringWithin answers "about to lapse" only. An already-lapsed
	// waiver belongs to WaiversLapsed — NOT to nobody, which is what an earlier
	// version of this file asserted. See TestWaiversLapsed.
	t.Run("already expired belongs to WaiversLapsed, not here", func(t *testing.T) {
		entries := horizonTestEntries(now.Add(-time.Hour))
		if got := WaiversExpiringWithin(entries, now, DefaultWaiverWarningWindow); len(got) != 0 {
			t.Fatalf("got %d upcoming warnings for an EXPIRED waiver, want 0", len(got))
		}
		if got := WaiversLapsed(entries, now); len(got) != 1 {
			t.Fatalf("WaiversLapsed got %d, want 1 — an expired waiver must be reported by SOMETHING", len(got))
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

// TestLapsedWaiverIsReportedNotSilent is the regression for the hole this file
// briefly had: the daily report is the ONLY surface that runs this package on a
// schedule (ci.yml triggers on push-to-main and pull_request; this fork works on
// carry/operational and Nightly does not cover this package). So if the report
// omits lapsed waivers, the check goes quiet at exactly the moment the situation
// becomes an outage.
func TestLapsedWaiverIsReportedNotSilent(t *testing.T) {
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	entries := horizonTestEntries(now.Add(-12 * 24 * time.Hour))

	report := FormatWaiverReport(
		WaiversLapsed(entries, now),
		WaiversExpiringWithin(entries, now, DefaultWaiverWarningWindow),
		now, DefaultWaiverWarningWindow)

	if report == "" {
		t.Fatal("a LAPSED waiver produced an empty report — the check went silent during an outage")
	}
	for _, want := range []string{"LAPSED", "ALREADY EXPIRED", "12 days AGO", "outage, not a horizon"} {
		if !strings.Contains(report, want) {
			t.Fatalf("lapsed report missing %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "UPCOMING") {
		t.Fatalf("a lapsed-only report must not claim an upcoming horizon:\n%s", report)
	}
}

func TestFormatWaiverReport(t *testing.T) {
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)

	// Empty MUST render empty: the caller treats "" as "raise no notification".
	// A periodic all-clear is how a channel becomes unread, which is the exact
	// failure this mechanism exists to prevent.
	if got := FormatWaiverReport(nil, nil, now, DefaultWaiverWarningWindow); got != "" {
		t.Fatalf("empty warnings rendered %q, want empty", got)
	}

	got := FormatWaiverReport(nil,
		WaiversExpiringWithin(horizonTestEntries(now.Add(3*24*time.Hour)), now, DefaultWaiverWarningWindow),
		now, DefaultWaiverWarningWindow)
	for _, want := range []string{"expire within 14 days", "2026-08-27", "ga-80po0c.3", "EVERY pull", "Do not bump it bare"} {
		if !strings.Contains(got, want) {
			t.Fatalf("report missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "LAPSED") {
		t.Fatalf("an upcoming-only report must not claim a lapse:\n%s", got)
	}
}

// A report that misstates its own window is believed, so pin it.
func TestFormatWaiverExpiryWarningsReportsTheWindowItWasGiven(t *testing.T) {
	now := time.Date(2026, time.August, 24, 12, 0, 0, 0, time.UTC)
	window := 60 * 24 * time.Hour
	got := FormatWaiverReport(nil,
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
	now := time.Now()
	report := FormatWaiverReport(
		WaiversLapsed(Catalog(), now),
		WaiversExpiringWithin(Catalog(), now, DefaultWaiverWarningWindow),
		now, DefaultWaiverWarningWindow)
	if report == "" {
		return
	}
	t.Log("\n" + report)
}
