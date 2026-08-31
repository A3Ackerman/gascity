package git

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// LocalRefNames returns the branch and tag names a repository already knows,
// read from disk — packed-refs and loose refs both — without shelling out or
// touching the network. Remote-tracking names are stripped of their remote
// segment (refs/remotes/origin/foo → foo, the remote HEAD symref skipped);
// tags lose their peel suffix. The result is the name vocabulary a GitHub
// tree URL's ref segment is drawn from, which is what callers recovering a
// ref/path boundary need. A worktree-style .git file is followed through its
// gitdir pointer.
func LocalRefNames(repoDir string) ([]string, error) {
	gitDir, err := resolveGitDir(repoDir)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]struct{})
	var names []string
	add := func(fullRef string) {
		name, ok := refDisplayName(fullRef)
		if !ok {
			return
		}
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}

	if data, err := os.ReadFile(filepath.Join(gitDir, "packed-refs")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "^") {
				continue
			}
			if _, ref, ok := strings.Cut(line, " "); ok {
				add(strings.TrimSpace(ref))
			}
		}
	}

	refsRoot := filepath.Join(gitDir, "refs")
	walkErr := filepath.WalkDir(refsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // unreadable subtrees just contribute no names
		}
		if rel, relErr := filepath.Rel(gitDir, path); relErr == nil {
			add(filepath.ToSlash(rel))
		}
		return nil
	})
	if walkErr != nil && len(names) == 0 {
		return nil, walkErr
	}
	return names, nil
}

// refDisplayName maps a full ref path to the name a tree URL would carry.
func refDisplayName(fullRef string) (string, bool) {
	switch {
	case strings.HasPrefix(fullRef, "refs/tags/"):
		return strings.TrimSuffix(strings.TrimPrefix(fullRef, "refs/tags/"), "^{}"), true
	case strings.HasPrefix(fullRef, "refs/heads/"):
		return strings.TrimPrefix(fullRef, "refs/heads/"), true
	case strings.HasPrefix(fullRef, "refs/remotes/"):
		rest := strings.TrimPrefix(fullRef, "refs/remotes/")
		_, name, ok := strings.Cut(rest, "/")
		if !ok || name == "HEAD" || name == "" {
			return "", false
		}
		return name, true
	default:
		return "", false
	}
}

// resolveGitDir locates the actual git directory for repoDir, following a
// worktree-style .git pointer file.
func resolveGitDir(repoDir string) (string, error) {
	gitPath := filepath.Join(repoDir, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", fmt.Errorf("locating git dir for %q: %w", repoDir, err)
	}
	if info.IsDir() {
		return gitPath, nil
	}
	data, err := os.ReadFile(gitPath)
	if err != nil {
		return "", fmt.Errorf("reading gitdir pointer for %q: %w", repoDir, err)
	}
	target := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(data)), "gitdir:"))
	if target == "" {
		return "", fmt.Errorf("empty gitdir pointer for %q", repoDir)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(repoDir, target)
	}
	return target, nil
}
