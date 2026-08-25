package providerledger

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// DefaultWaiverWarningWindow is how far ahead of a waiver expiry the advance
// warning fires.
//
// The expiry itself is a hard gate: Validate fails the moment it passes, and
// because it is checked against wall-clock time that failure lands on every
// pull request in the repo at once, regardless of what the change touches. On
// 2026-08-12 that is exactly what happened and nobody noticed for twelve days
// (ga-9hrf8b), because this repo runs CI on pull_request only and sees few PRs
// — so the deadline arrived with no surface to announce it.
//
// Two weeks is chosen to be longer than a plausible gap between PRs here, so
// the warning is seen by somebody before it becomes everybody's problem.
const DefaultWaiverWarningWindow = 14 * 24 * time.Hour

// WaiverExpiryWarning is one waiver approaching its expiry.
type WaiverExpiryWarning struct {
	EntryID     string
	Constructor string
	Owner       string
	Expires     time.Time
	Reason      string
}

// WaiversExpiringWithin returns waivers whose expiry falls inside
// [now, now+window) — the ones about to lapse. Already-lapsed waivers are
// reported by WaiversLapsed instead; callers are expected to render BOTH (see
// the note on WaiversLapsed for why excluding the lapsed ones is a trap).
//
// Sorted by expiry then entry ID so repeated runs render identically and a
// diffing consumer sees no spurious churn.
func WaiversExpiringWithin(entries []Entry, now time.Time, window time.Duration) []WaiverExpiryWarning {
	deadline := now.Add(window)
	return collectWaivers(entries, func(w *Waiver) bool {
		return !w.Expires.Before(now) && w.Expires.Before(deadline)
	})
}

// WaiversLapsed returns waivers that have ALREADY expired.
//
// These are included in the daily report deliberately, reversing an earlier
// decision of mine to exclude them "because Validate is already loud about
// them". Validate's loudness is exactly what failed for twelve days:
// .github/workflows/ci.yml triggers on `push: branches: [main]` and
// `pull_request`, this fork works on carry/operational, and Nightly's only
// `go test` is ./test/acceptance/tier_c/... — so nothing runs this package
// except a pull request, and PRs here are rare.
//
// Follow that through and the exclusion inverts: the T-14..T-1 window would be
// reported daily and visible, and T-0 onward — the state that is actively
// reddening every PR in the repo — would be reported by nothing until someone
// happened to open one. The check would go QUIET at the moment the situation
// became an outage.
//
// The noise objection that motivated the exclusion is already answered by the
// caller's design: it opens-or-UPDATES a single issue, so a lapsed waiver keeps
// one issue current. That is not spam, it is an open incident staying open.
// Escalate rather than exclude.
func WaiversLapsed(entries []Entry, now time.Time) []WaiverExpiryWarning {
	return collectWaivers(entries, func(w *Waiver) bool {
		return w.Expires.Before(now)
	})
}

func collectWaivers(entries []Entry, keep func(*Waiver) bool) []WaiverExpiryWarning {
	var out []WaiverExpiryWarning
	for _, entry := range entries {
		for _, claim := range entry.Claims {
			w := claim.Waiver
			if w == nil || !keep(w) {
				continue
			}
			out = append(out, WaiverExpiryWarning{
				EntryID:     entry.ID,
				Constructor: renderSymbolRef(claim.Constructor),
				Owner:       w.Owner,
				Expires:     w.Expires,
				Reason:      w.Reason,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Expires.Equal(out[j].Expires) {
			return out[i].Expires.Before(out[j].Expires)
		}
		if out[i].EntryID != out[j].EntryID {
			return out[i].EntryID < out[j].EntryID
		}
		return out[i].Constructor < out[j].Constructor
	})
	return out
}

// FormatWaiverReport renders the operator-facing report, or "" when there is
// nothing to say. The empty string is the signal to callers that no
// notification should be raised at all — a periodic "nothing to report" is how
// a channel becomes unread, which is the failure this mechanism exists to
// prevent.
//
// LAPSED waivers are rendered FIRST and as an outage, because that is what they
// are: while one is expired, Validate(Catalog) fails on every pull request in
// the repo. Upcoming expiries follow as a horizon.
//
// The window is passed in rather than read from DefaultWaiverWarningWindow so
// the header cannot claim a horizon the caller did not use — a report that
// misstates its own window is worse than no report, because it is believed.
func FormatWaiverReport(lapsed, upcoming []WaiverExpiryWarning, now time.Time, window time.Duration) string {
	if len(lapsed) == 0 && len(upcoming) == 0 {
		return ""
	}
	var b strings.Builder

	if len(lapsed) > 0 {
		fmt.Fprintf(&b, "LAPSED: %d provider-ledger waiver(s) have ALREADY EXPIRED as of %s.\n",
			len(lapsed), now.UTC().Format("2006-01-02"))
		b.WriteString("Validate(Catalog) is failing RIGHT NOW, which reds CI for EVERY pull request\n")
		b.WriteString("in this repo until it is dealt with — including changes that touch nothing\n")
		b.WriteString("related. This is an outage, not a horizon.\n\n")
		writeWaiverLines(&b, lapsed, now)
		b.WriteString("\n")
	}

	if len(upcoming) > 0 {
		fmt.Fprintf(&b, "UPCOMING: %d provider-ledger waiver(s) expire within %d days of %s.\n",
			len(upcoming), int(window.Hours()/24), now.UTC().Format("2006-01-02"))
		b.WriteString("When they expire, Validate(Catalog) fails and CI goes red for EVERY pull\n")
		b.WriteString("request in this repo until it is dealt with.\n\n")
		writeWaiverLines(&b, upcoming, now)
		b.WriteString("\n")
	}

	b.WriteString("Either land the contract work and delete the waiver, or bump the horizon\n")
	b.WriteString("WITH a recorded reason (see runtimeContractWaiverExpiry in ledger.go).\n")
	b.WriteString("Do not bump it bare: the expiry is the only mechanism making this debt visible.\n")
	return b.String()
}

func writeWaiverLines(b *strings.Builder, warnings []WaiverExpiryWarning, now time.Time) {
	for _, w := range warnings {
		days := int(w.Expires.Sub(now).Hours() / 24)
		when := fmt.Sprintf("%d days", days)
		if days < 0 {
			when = fmt.Sprintf("%d days AGO", -days)
		}
		fmt.Fprintf(b, "  %s  expires %s (%s)  owner %s\n    constructor %s\n    reason: %s\n",
			w.EntryID, w.Expires.UTC().Format("2006-01-02"), when, w.Owner, w.Constructor, w.Reason)
	}
}
