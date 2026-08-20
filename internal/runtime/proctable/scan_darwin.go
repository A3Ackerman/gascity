//go:build darwin

package proctable

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/gastownhall/gascity/internal/runtime"
)

// ScanBySessionID returns live agent root processes whose environment carries
// GC_SESSION_ID equal to id. Empty id returns all roots with any GC_SESSION_ID.
func ScanBySessionID(id string) ([]runtime.LiveRuntime, error) {
	if err := liveScanGuard(); err != nil {
		return []runtime.LiveRuntime{}, err
	}
	records, err := psRecords()
	if err != nil {
		return []runtime.LiveRuntime{}, err
	}
	return scanDarwinRecords(records, id), nil
}

// scanDarwinRecords selects the agent roots matching id from an already-read
// process table. It is split out of ScanBySessionID so the selection policy —
// in particular the infrastructure exclusion below, which is load-bearing
// against a whole-city outage — is testable against a fixture instead of
// requiring a live `ps` and a live tmux server.
func scanDarwinRecords(records map[int]psRecord, id string) []runtime.LiveRuntime {
	var out []runtime.LiveRuntime
	for _, record := range records {
		// Exclude infrastructure processes from being reported as agent roots
		// at all. This is the fix for the hq-nie0 / ga-03ixvj whole-city
		// outage, and on darwin it is the ONLY thing that closes it.
		//
		// A tmux server has no GC_SESSION_ID in its real environment — but
		// `ps eww` prints argv and env concatenated into one field, and the
		// server permanently retains the argv of the `new-session` that
		// founded it, which carries `-e GC_SESSION_ID=<mayor session>` flag
		// values. parseInlineEnv cannot tell a `KEY=VALUE` that is an
		// environment entry from one that is a `-e` flag argument, so the
		// server READS as a process in the mayor's session. Measured on the
		// live server: exactly one `GC_SESSION_ID=` in the full `ps -Eww`
		// output, at byte offset 1561, inside an argv region that ends at
		// ~4030 — i.e. in argv, never in env.
		//
		// That misreading is why scrubbing the environment cannot fix this:
		// there is nothing in the environment to scrub. The server's parent is
		// launchd, so the parent-dedup below never fires either, and once the
		// mayor's tmux SESSION is gone (post-handoff) IsTracked is false and
		// the server is killed as an orphan — taking every session on the
		// socket with it. One socket = one server = the whole city.
		if record.pid <= 1 || isInfrastructureCommand(record.command) {
			continue
		}
		sessionID := record.env["GC_SESSION_ID"]
		if sessionID == "" {
			continue
		}
		if id != "" && sessionID != id {
			continue
		}
		if parent, ok := records[record.ppid]; ok && parent.env["GC_SESSION_ID"] == sessionID && !isInfrastructureCommand(parent.command) {
			continue
		}
		epoch, _ := strconv.Atoi(record.env["GC_RUNTIME_EPOCH"])
		city := record.env["GC_CITY_PATH"]
		if city == "" {
			city = record.env["GC_CITY"]
		}
		out = append(out, runtime.LiveRuntime{
			SessionID: sessionID,
			City:      city,
			Epoch:     epoch,
			PID:       record.pid,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].PID < out[j].PID
	})
	if out == nil {
		out = []runtime.LiveRuntime{}
	}
	return out
}

// IsScanRoot reports whether pid is outside its GC_SESSION_ID parent's
// envelope and should be treated as an agent root.
func IsScanRoot(pid int) bool {
	if err := liveScanGuard(); err != nil {
		return false
	}
	if pid == 1 {
		return true
	}
	if pid <= 0 {
		return false
	}
	if pid == os.Getpid() {
		return false
	}
	records, err := psRecords()
	if err != nil {
		return false
	}
	record, ok := records[pid]
	// Same infrastructure exclusion as scanDarwinRecords: a tmux server must
	// never be classified as an agent root (see the note there).
	if !ok || isInfrastructureCommand(record.command) {
		return false
	}
	sessionID := record.env["GC_SESSION_ID"]
	if sessionID == "" {
		return false
	}
	parent, ok := records[record.ppid]
	return !ok || parent.env["GC_SESSION_ID"] != sessionID || isInfrastructureCommand(parent.command)
}

type psRecord struct {
	pid     int
	ppid    int
	command string
	env     map[string]string
}

func psRecords() (map[int]psRecord, error) {
	out, err := exec.Command("ps", "eww", "-ax", "-o", "pid=,ppid=,command=").Output()
	if err != nil {
		return nil, fmt.Errorf("running ps: %w", err)
	}
	records := make(map[int]psRecord)
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		records[pid] = psRecord{
			pid:     pid,
			ppid:    ppid,
			command: darwinPSCommand(fields),
			env:     parseInlineEnv(fields[2:]),
		}
	}
	return records, nil
}

func parseInlineEnv(fields []string) map[string]string {
	env := make(map[string]string)
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok || key == "" {
			continue
		}
		env[key] = value
	}
	return env
}
