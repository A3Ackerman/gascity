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

// WaiversExpiringWithin returns every waiver in entries whose expiry falls
// inside [now, now+window), sorted by expiry then entry ID so repeated runs
// render identically and a diffing consumer sees no spurious churn.
//
// ALREADY-EXPIRED waivers are deliberately EXCLUDED: those are Validate's job
// and they are already failing the build loudly. This function answers the
// narrower question "what is about to break", which is the one nothing was
// asking on 2026-08-12.
func WaiversExpiringWithin(entries []Entry, now time.Time, window time.Duration) []WaiverExpiryWarning {
	var out []WaiverExpiryWarning
	deadline := now.Add(window)
	for _, entry := range entries {
		for _, claim := range entry.Claims {
			w := claim.Waiver
			if w == nil || w.Expires.Before(now) {
				continue
			}
			if !w.Expires.Before(deadline) {
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

// FormatWaiverExpiryWarnings renders warnings as an operator-facing report, or
// "" when there is nothing to say. The empty string is the signal to callers
// that no notification should be raised at all — a periodic "nothing to report"
// is how a channel becomes unread, which is the failure this whole mechanism
// exists to prevent.
// The window is passed in rather than read from DefaultWaiverWarningWindow so
// the header cannot claim a horizon the caller did not use — a report that
// misstates its own window is worse than no report, because it is believed.
func FormatWaiverExpiryWarnings(warnings []WaiverExpiryWarning, now time.Time, window time.Duration) string {
	if len(warnings) == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d provider-ledger waiver(s) expire within %d days of %s.\n",
		len(warnings), int(window.Hours()/24), now.UTC().Format("2006-01-02"))
	b.WriteString("When they expire, Validate(Catalog) fails and CI goes red for EVERY pull\n")
	b.WriteString("request in this repo until it is dealt with.\n\n")
	for _, w := range warnings {
		days := int(w.Expires.Sub(now).Hours() / 24)
		fmt.Fprintf(&b, "  %s  expires %s (%d days)  owner %s\n    constructor %s\n    reason: %s\n",
			w.EntryID, w.Expires.UTC().Format("2006-01-02"), days, w.Owner, w.Constructor, w.Reason)
	}
	b.WriteString("\nEither land the contract work and delete the waiver, or bump the horizon\n")
	b.WriteString("WITH a recorded reason (see runtimeContractWaiverExpiry in ledger.go).\n")
	b.WriteString("Do not bump it bare: the expiry is the only mechanism making this debt visible.\n")
	return b.String()
}
