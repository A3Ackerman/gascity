//go:build darwin

package proctable

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/gastownhall/gascity/internal/runtime"
)

// Scan returns live agent root processes matching target.
func Scan(target runtime.ProcessTarget) ([]runtime.LiveRuntime, error) {
	if err := liveScanGuard(); err != nil {
		return []runtime.LiveRuntime{}, err
	}
	records, err := psRecords()
	if err != nil {
		return []runtime.LiveRuntime{}, identityObservationError("reading process table", err)
	}
	if err := populateDarwinCWDs(records, target); err != nil {
		return scanDarwinRecords(records, target), err
	}
	return scanDarwinRecords(records, target), nil
}

func populateDarwinCWDs(records map[int]psRecord, target runtime.ProcessTarget) error {
	if target.WorkDir == "" || target.Alias == "" && len(target.ProcessNames) == 0 {
		return nil
	}
	pidSet := make(map[int]struct{})
	for _, record := range records {
		if !processNeedsWorkDir(record.env, record.argv, target) {
			continue
		}
		pidSet[record.pid] = struct{}{}
		if parent, ok := records[record.ppid]; ok && processNeedsWorkDir(parent.env, parent.argv, target) {
			pidSet[parent.pid] = struct{}{}
		}
	}
	if len(pidSet) == 0 {
		return nil
	}
	pids := make([]int, 0, len(pidSet))
	for pid := range pidSet {
		pids = append(pids, pid)
	}
	slices.Sort(pids)
	rawPIDs := make([]string, 0, len(pids))
	for _, pid := range pids {
		rawPIDs = append(rawPIDs, strconv.Itoa(pid))
	}
	out, cmdErr := exec.Command("/usr/sbin/lsof", "-a", "-d", "cwd", "-Fn", "-p", strings.Join(rawPIDs, ",")).Output()
	cwds := parseDarwinCWDs(string(out))
	for pid := range pidSet {
		cwd, ok := cwds[pid]
		if !ok {
			if err := syscall.Kill(pid, 0); errors.Is(err, syscall.ESRCH) {
				delete(records, pid)
				continue
			}
			return fmt.Errorf("%w: reading cwd for pid %d: %w", runtime.ErrProcessIdentityIncomplete, pid, cmdErr)
		}
		record, ok := records[pid]
		if !ok {
			continue
		}
		record.cwd = cwd
		records[pid] = record
	}
	return nil
}

func parseDarwinCWDs(output string) map[int]string {
	cwds := make(map[int]string)
	pid := 0
	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.HasPrefix(line, "p"):
			pid, _ = strconv.Atoi(strings.TrimPrefix(line, "p"))
		case pid > 0 && strings.HasPrefix(line, "n"):
			cwds[pid] = strings.TrimPrefix(line, "n")
		}
	}
	return cwds
}

func scanDarwinRecords(records map[int]psRecord, target runtime.ProcessTarget) []runtime.LiveRuntime {
	var out []runtime.LiveRuntime
	for _, record := range records {
		if record.pid <= 1 || isInfrastructureCommand(record.command) || !recordMatchesTarget(record, target) {
			continue
		}
		if parent, ok := records[record.ppid]; ok && recordMatchesTarget(parent, target) && !isInfrastructureCommand(parent.command) {
			continue
		}
		epoch, _ := strconv.Atoi(record.env["GC_RUNTIME_EPOCH"])
		city := record.env["GC_CITY_PATH"]
		if city == "" {
			city = record.env["GC_CITY"]
		}
		out = append(out, runtime.LiveRuntime{
			SessionID:   record.env["GC_SESSION_ID"],
			Alias:       record.env["GC_ALIAS"],
			City:        city,
			WorkDir:     record.cwd,
			ProcessName: matchingProcessName(record.argv, target.ProcessNames),
			Epoch:       epoch,
			PID:         record.pid,
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

func recordMatchesTarget(record psRecord, target runtime.ProcessTarget) bool {
	return processMatchesTarget(record.env, record.cwd, record.argv, target)
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
	argv    []string
	cwd     string
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
			argv:    darwinPSArgv(fields),
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
