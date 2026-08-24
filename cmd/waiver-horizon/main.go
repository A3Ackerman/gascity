// Command waiver-horizon reports provider-ledger waivers that are about to
// expire.
//
// It exists because the expiry is a wall-clock gate on every pull request in
// this repo: when a waiver lapses, Validate(Catalog) fails and CI goes red for
// everyone regardless of what their change touches. On 2026-08-12 that happened
// and went unnoticed for twelve days (ga-9hrf8b) — CI here runs on
// pull_request only, so nothing was watching the calendar.
//
// Prints a report to stdout when something expires inside the warning window,
// and prints NOTHING otherwise. Callers treat empty output as "raise no
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

	report := providerledger.FormatWaiverExpiryWarnings(
		providerledger.WaiversExpiringWithin(providerledger.Catalog(), time.Now(), *window),
		time.Now(), *window,
	)
	if report == "" {
		return
	}
	fmt.Fprint(os.Stdout, report)
}
