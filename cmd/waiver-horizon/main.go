// Command waiver-horizon reports provider-ledger waivers that are about to
// expire.
//
// It exists because the expiry is a wall-clock gate on every pull request in
// this repo: when a waiver lapses, Validate(Catalog) fails and CI goes red for
// everyone regardless of what their change touches. On 2026-08-12 that happened
// and went unnoticed for twelve days (ga-9hrf8b) — CI here runs on
// pull_request only, so nothing was watching the calendar.
//
// Prints a report to stdout when a waiver has already lapsed OR expires inside
// the warning window, and prints NOTHING otherwise. Lapsed waivers are included
// deliberately: CI here runs on pull_request only, so after an expiry nothing
// else announces it until somebody opens a PR. Callers treat empty output as "raise no
// notification": a periodic all-clear is how a channel stops being read, which
// is the failure this command exists to prevent.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/gastownhall/gascity/internal/testutil/providerledger"
)

func main() {
	window := flag.Duration("window", providerledger.DefaultWaiverWarningWindow,
		"how far ahead to look for expiring waivers")
	flag.Parse()

	now := time.Now()
	catalog := providerledger.Catalog()
	report := providerledger.FormatWaiverReport(
		providerledger.WaiversLapsed(catalog, now),
		providerledger.WaiversExpiringWithin(catalog, now, *window),
		now, *window,
	)
	if report == "" {
		return
	}
	// A short write to stdout is a real failure mode for this command: the
	// workflow reads the report off stdout and decides whether to raise an
	// incident from it, so a silently truncated write would understate a lapse
	// in the one channel that is supposed to announce it.
	if _, err := fmt.Fprint(os.Stdout, report); err != nil {
		fmt.Fprintf(os.Stderr, "waiver-horizon: writing report: %v\n", err) //nolint:errcheck // stderr is best-effort
		os.Exit(1)
	}
}
