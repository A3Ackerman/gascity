package main

import (
	"fmt"
	"io"
)

// managedDoltGlobalSetupSQL is applied to every managed Dolt server once it is
// query-ready.
//
// WHY THIS IS NOT IN THE CONFIG FILE, which is where it obviously belongs.
// dolt 2.2.4 IGNORES dolt_transaction_commit in the server config's
// system_variables block. Measured on the live server 2026-08-26: with
// dolt_transaction_commit set to ON in the generated config and the server
// started from that exact file, @@GLOBAL.dolt_transaction_commit read 0, while
// dolt_stats_enabled, dolt_stats_paused, dolt_auto_gc_enabled and wait_timeout
// FROM THE SAME BLOCK all took effect. So the block works and this one variable
// is specifically not applied. Persisting it is no better: SET PERSIST writes
// sqlserver.global.dolt_transaction_commit into ~/.dolt/config_global.json, and
// a server started with --config does not read it back -- verified across a
// real restart, where the value returned to 0. Setting it globally after
// readiness is the only delivery that works, so it must be re-applied on every
// managed start.
//
// WHY IT MATTERS. With this OFF, a SQL transaction commit never becomes a Dolt
// version commit. beads' post-run auto-commit hook returns immediately outside
// embedded mode -- "Skips SQL server modes; the server owns transaction commit
// lifecycle there" -- so on a server that does not own it, NEITHER side
// commits. 23 of 27 bd write-command files rely on that hook (memory, state,
// gate, promote, merge_slot, config, migrate, ...); only batch, delete and
// label route through the transactional helper. Their rows land in the working
// set and stay there forever, and a dirty working set blocks every subsequent
// merge -- which wedged qcore hub-sync three times in three days (ga-7unsv0).
// bd remember reports success, writes its row, and creates no commit.
//
// Proven in an isolated dolt 2.2.4 server before shipping: a plain INSERT now
// commits and leaves the working set clean; the BeginTx + DOLT_COMMIT(msg) +
// COMMIT shape beads' RunInTransaction uses keeps its descriptive message with
// delta exactly one commit and no doubling; the qcore hub syncer's merge batch
// is unaffected, fast-forward included. Cost: the previously-stranding paths
// commit as the generic "Transaction commit".
var managedDoltGlobalSetupSQL = []struct {
	name string
	stmt string
}{
	{"dolt_transaction_commit", "SET GLOBAL dolt_transaction_commit = 1"},
}

// managedDoltGlobalSetupExecFn is a seam so the fail-visible path can be
// tested. A warning nobody has ever seen fire is a warning that may not fire.
var managedDoltGlobalSetupExecFn = func(host, port, user, stmt string) error {
	_, err := runManagedDoltSQL(host, port, user, "-q", stmt)
	return err
}

// applyManagedDoltGlobalSetup runs the post-readiness global settings against a
// freshly started managed server.
//
// FAIL VISIBLE, NOT FAIL CLOSED. A failure here is reported on stderr and the
// server still starts. Refusing to start would trade "writes do not commit" for
// "there is no data plane at all", which is strictly worse. But it is never
// silent: a caller that sees no warning can rely on the setting having applied.
func applyManagedDoltGlobalSetup(host, port, user string, stderr io.Writer) {
	for _, s := range managedDoltGlobalSetupSQL {
		if err := managedDoltGlobalSetupExecFn(host, port, user, s.stmt); err != nil {
			fmt.Fprintf(stderr, "managed-dolt: failed to apply %s (%v) -- bd writes that rely on the client-side auto-commit hook will NOT be committed and will block merges; see ga-7unsv0\n", s.name, err) //nolint:errcheck
		}
	}
}
