//go:build darwin

package proctable

import (
	"strings"
	"testing"
)

// TestScanDarwinRecordsExcludesInfrastructureRoot is the direct regression test
// for the hq-nie0 / ga-03ixvj whole-city outage: a tmux server that reads as a
// member of the mayor's session must never be returned as an agent root, or the
// orphan sweep will kill it and take every session on the socket with it.
func TestScanDarwinRecordsExcludesInfrastructureRoot(t *testing.T) {
	records := map[int]psRecord{
		100: {
			pid:     100,
			ppid:    1, // reparented to launchd — the parent-dedup below never fires
			command: "tmux: server",
			env:     map[string]string{"GC_SESSION_ID": "hq-session"},
		},
		101: {
			pid:     101,
			ppid:    100,
			command: "claude",
			env:     map[string]string{"GC_SESSION_ID": "hq-session"},
		},
	}

	got := scanDarwinRecords(records, "hq-session")
	if len(got) != 1 || got[0].PID != 101 {
		t.Fatalf("scanDarwinRecords = %+v, want only agent pid 101 (the tmux server must be excluded)", got)
	}
}

// TestPSRecordConflatesFoundingArgvIntoEnv pins the MECHANISM, so nobody
// "fixes" this by scrubbing an environment variable that was never there.
//
// `ps eww -ax -o pid=,ppid=,command=` prints argv and environ concatenated into
// a single field. A tmux server permanently retains the argv of the
// `new-session` invocation that founded it, and gc passes session environment
// through `-e KEY=VALUE` flags — so parseInlineEnv, which cannot distinguish a
// real environment entry from a `-e` flag argument, reports GC_SESSION_ID for a
// process whose actual environment has none.
//
// The fields below are the shape measured on the live city's tmux server: one
// GC_SESSION_ID occurrence in the whole `ps -Eww` output, sitting in the argv
// region, never in the environment region.
func TestPSRecordConflatesFoundingArgvIntoEnv(t *testing.T) {
	line := "12611 1 tmux -u -L qlandia new-session -d -s mayor " +
		"-c /city -e GC_AGENT=mayor -e GC_SESSION_ID=hq-wisp-hbl68qx -e GC_CITY_PATH=/city " +
		"USER=someone LANG=en_US.UTF-8"
	fields := strings.Fields(line)

	record := psRecord{
		pid:     12611,
		ppid:    1,
		command: darwinPSCommand(fields),
		env:     parseInlineEnv(fields[2:]),
	}

	if got := record.env["GC_SESSION_ID"]; got != "hq-wisp-hbl68qx" {
		t.Fatalf("env[GC_SESSION_ID] = %q; the conflation this guard defends against is gone — "+
			"re-check whether the exclusion is still required", got)
	}
	if !isInfrastructureCommand(record.command) {
		t.Fatalf("isInfrastructureCommand(%q) = false, want true", record.command)
	}

	got := scanDarwinRecords(map[int]psRecord{12611: record}, "hq-wisp-hbl68qx")
	if len(got) != 0 {
		t.Fatalf("scanDarwinRecords surfaced the tmux server as an agent root: %+v", got)
	}
}

// TestScanDarwinRecordsStillReturnsGenuineOrphans proves the exclusion is not
// over-broad: an escaped agent root with no live parent is still surfaced, so
// the orphan sweep keeps working.
func TestScanDarwinRecordsStillReturnsGenuineOrphans(t *testing.T) {
	records := map[int]psRecord{
		200: {
			pid:     200,
			ppid:    1,
			command: "claude",
			env: map[string]string{
				"GC_SESSION_ID":    "hq-session",
				"GC_CITY_PATH":     "/city",
				"GC_RUNTIME_EPOCH": "6",
			},
		},
	}

	got := scanDarwinRecords(records, "hq-session")
	if len(got) != 1 || got[0].PID != 200 {
		t.Fatalf("scanDarwinRecords = %+v, want the orphaned agent root pid 200", got)
	}
	if got[0].City != "/city" || got[0].Epoch != 6 {
		t.Fatalf("scanDarwinRecords lost record fields: %+v", got[0])
	}
}
