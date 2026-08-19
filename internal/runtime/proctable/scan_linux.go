//go:build linux

package proctable

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/gastownhall/gascity/internal/runtime"
)

// Scan returns live agent root processes matching target.
func Scan(target runtime.ProcessTarget) ([]runtime.LiveRuntime, error) {
	if err := liveScanGuard(); err != nil {
		return []runtime.LiveRuntime{}, err
	}
	return scanWithRoot(scanRoot, target)
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
	if pid <= 0 || pid == os.Getpid() || isInfrastructureProcess(scanRoot, pid) {
		return false
	}
	env, err := parseEnvironFile(filepath.Join(scanRoot, strconv.Itoa(pid), "environ"))
	if err != nil || len(env) == 0 {
		return false
	}
	sessionID := env["GC_SESSION_ID"]
	if sessionID == "" {
		return false
	}
	cwd, err := readProcessCWD(scanRoot, pid)
	if err != nil {
		return false
	}
	target := runtime.ProcessTarget{SessionID: sessionID}
	isRoot, err := isRootWithTarget(scanRoot, pid, target, env, cwd, nil)
	return err == nil && isRoot
}

func scanWithRoot(root string, target runtime.ProcessTarget) ([]runtime.LiveRuntime, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return []runtime.LiveRuntime{}, identityObservationError("enumerating "+root, err)
	}

	var (
		out     []runtime.LiveRuntime
		scanErr error
	)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid <= 1 || isInfrastructureProcess(root, pid) {
			continue
		}
		env, err := parseEnvironFile(filepath.Join(root, entry.Name(), "environ"))
		if err != nil {
			scanErr = errors.Join(scanErr, fmt.Errorf("reading environ for pid %d: %w", pid, err))
			continue
		}
		if root == "/proc" && pid == os.Getpid() {
			env = mergeCurrentEnv(env)
		}
		if len(env) == 0 {
			continue
		}
		var argv []string
		if processNeedsArgv(env, target) {
			argv, err = readProcessArgv(root, pid)
			if err != nil {
				scanErr = errors.Join(scanErr, fmt.Errorf("%w: reading argv for pid %d: %w", runtime.ErrProcessIdentityIncomplete, pid, err))
				continue
			}
		}
		cwd := ""
		if processNeedsWorkDir(env, argv, target) {
			cwd, err = readProcessCWD(root, pid)
			if err != nil {
				scanErr = errors.Join(scanErr, fmt.Errorf("%w: reading cwd for pid %d: %w", runtime.ErrProcessIdentityIncomplete, pid, err))
				continue
			}
		}
		if !processMatchesTarget(env, cwd, argv, target) {
			continue
		}
		rootProcess, err := isRootWithTarget(root, pid, target, env, cwd, argv)
		if err != nil {
			scanErr = errors.Join(scanErr, fmt.Errorf("checking root for pid %d: %w", pid, err))
			continue
		}
		if !rootProcess {
			continue
		}
		epoch, _ := strconv.Atoi(env["GC_RUNTIME_EPOCH"])
		city := env["GC_CITY_PATH"]
		if city == "" {
			city = env["GC_CITY"]
		}
		out = append(out, runtime.LiveRuntime{
			SessionID:   env["GC_SESSION_ID"],
			Alias:       env["GC_ALIAS"],
			City:        city,
			WorkDir:     cwd,
			ProcessName: matchingProcessName(argv, target.ProcessNames),
			Epoch:       epoch,
			PID:         pid,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].PID < out[j].PID
	})
	if out == nil {
		out = []runtime.LiveRuntime{}
	}
	return out, scanErr
}

func mergeCurrentEnv(env map[string]string) map[string]string {
	if env == nil {
		env = make(map[string]string)
	}
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		env[key] = value
	}
	return env
}

func parseEnvironFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) || os.IsPermission(err) {
			return nil, nil
		}
		return nil, err
	}
	env := make(map[string]string)
	for _, entry := range strings.Split(string(data), "\x00") {
		if entry == "" {
			continue
		}
		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		env[key] = value
	}
	return env, nil
}

func readProcessCWD(root string, pid int) (string, error) {
	cwd, err := os.Readlink(filepath.Join(root, strconv.Itoa(pid), "cwd"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return cwd, nil
}

func readProcessArgv(root string, pid int) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var argv []string
	for _, arg := range strings.Split(string(data), "\x00") {
		if arg != "" {
			argv = append(argv, arg)
		}
	}
	return argv, nil
}

func isRootWithTarget(root string, pid int, target runtime.ProcessTarget, env map[string]string, cwd string, argv []string) (bool, error) {
	ppid, ok, err := readParentPID(filepath.Join(root, strconv.Itoa(pid), "stat"))
	if err != nil {
		return false, err
	}
	if !ok {
		// stat vanished between environ read and here; process died in the race
		// window — skip rather than misreport it as a root.
		return false, nil
	}
	if ppid <= 1 {
		return true, nil
	}
	parentEnv, err := parseEnvironFile(filepath.Join(root, strconv.Itoa(ppid), "environ"))
	if err != nil {
		return false, err
	}
	var parentArgv []string
	if processNeedsArgv(parentEnv, target) {
		parentArgv, err = readProcessArgv(root, ppid)
		if err != nil {
			return false, fmt.Errorf("%w: reading parent argv: %w", runtime.ErrProcessIdentityIncomplete, err)
		}
	}
	parentCWD := ""
	if processNeedsWorkDir(parentEnv, parentArgv, target) {
		parentCWD, err = readProcessCWD(root, ppid)
		if err != nil {
			return false, fmt.Errorf("%w: reading parent cwd: %w", runtime.ErrProcessIdentityIncomplete, err)
		}
	}
	if processMatchesTarget(parentEnv, parentCWD, parentArgv, target) {
		return isInfrastructureProcess(root, ppid), nil
	}
	return processMatchesTarget(env, cwd, argv, target), nil
}

func isInfrastructureProcess(root string, pid int) bool {
	data, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "comm"))
	if err != nil {
		return false
	}
	command := strings.ToLower(strings.TrimSpace(string(data)))
	return strings.Contains(command, "tmux")
}

func readParentPID(path string) (int, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) || os.IsPermission(err) {
			return 0, false, nil
		}
		return 0, false, err
	}
	text := string(data)
	closeParen := strings.LastIndex(text, ")")
	if closeParen < 0 || closeParen+1 >= len(text) {
		return 0, false, fmt.Errorf("malformed stat file %s", path)
	}
	fields := strings.Fields(text[closeParen+1:])
	if len(fields) < 2 {
		return 0, false, fmt.Errorf("malformed stat file %s", path)
	}
	ppid, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, false, fmt.Errorf("parsing ppid from %s: %w", path, err)
	}
	return ppid, true, nil
}
