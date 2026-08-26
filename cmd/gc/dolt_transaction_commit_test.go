package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// The setting is the whole point of the file; if it is ever dropped, bd writes
// silently stop committing and wedge the next merge (ga-7unsv0). That failure
// is invisible at the call site, so pin it here.
func TestManagedDoltGlobalSetupIncludesTransactionCommit(t *testing.T) {
	var found string
	for _, s := range managedDoltGlobalSetupSQL {
		if s.name == "dolt_transaction_commit" {
			found = s.stmt
		}
	}
	if found == "" {
		t.Fatal("managedDoltGlobalSetupSQL has no dolt_transaction_commit entry")
	}
	// SET GLOBAL specifically: SET PERSIST writes a file the server does not
	// read back when started with --config, and the config file's
	// system_variables block ignores this variable outright.
	if !strings.Contains(strings.ToUpper(found), "SET GLOBAL") {
		t.Fatalf("statement = %q, want a SET GLOBAL", found)
	}
	if !strings.Contains(found, "dolt_transaction_commit = 1") {
		t.Fatalf("statement = %q, want it set to 1", found)
	}
}

func TestApplyManagedDoltGlobalSetupIsSilentOnSuccess(t *testing.T) {
	orig := managedDoltGlobalSetupExecFn
	t.Cleanup(func() { managedDoltGlobalSetupExecFn = orig })

	var gotHost, gotPort, gotUser, gotStmt string
	calls := 0
	managedDoltGlobalSetupExecFn = func(host, port, user, stmt string) error {
		calls++
		gotHost, gotPort, gotUser, gotStmt = host, port, user, stmt
		return nil
	}

	var buf bytes.Buffer
	applyManagedDoltGlobalSetup("127.0.0.1", "51361", "root", &buf)

	if calls != len(managedDoltGlobalSetupSQL) {
		t.Fatalf("exec calls = %d, want %d", calls, len(managedDoltGlobalSetupSQL))
	}
	if gotHost != "127.0.0.1" || gotPort != "51361" || gotUser != "root" {
		t.Fatalf("connection args = %q/%q/%q, want 127.0.0.1/51361/root", gotHost, gotPort, gotUser)
	}
	if !strings.Contains(gotStmt, "dolt_transaction_commit") {
		t.Fatalf("stmt = %q, want the transaction-commit setting", gotStmt)
	}
	if buf.Len() != 0 {
		t.Fatalf("stderr = %q, want silence on success", buf.String())
	}
}

// FAIL VISIBLE, NOT FAIL CLOSED: the server must still come up, but the
// operator must be told, because the consequence (writes that never commit) is
// otherwise indistinguishable from normal operation until a merge wedges hours
// later.
func TestApplyManagedDoltGlobalSetupWarnsLoudlyOnFailure(t *testing.T) {
	orig := managedDoltGlobalSetupExecFn
	t.Cleanup(func() { managedDoltGlobalSetupExecFn = orig })
	managedDoltGlobalSetupExecFn = func(_, _, _, _ string) error {
		return errors.New("connection refused")
	}

	var buf bytes.Buffer
	applyManagedDoltGlobalSetup("127.0.0.1", "51361", "root", &buf)

	out := buf.String()
	if out == "" {
		t.Fatal("stderr is empty; a failure to apply this setting must never be silent")
	}
	for _, want := range []string{"dolt_transaction_commit", "connection refused", "ga-7unsv0"} {
		if !strings.Contains(out, want) {
			t.Errorf("stderr = %q, want it to mention %q", out, want)
		}
	}
}
