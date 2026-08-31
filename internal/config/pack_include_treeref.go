package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gastownhall/gascity/internal/git"
	"github.com/gastownhall/gascity/internal/remotesource"
)

// cachedIncludeDir returns the directory inside a resolved repo cache that a
// remote include's subpath names, recovering the ref/subpath boundary for
// GitHub tree and blob sources whose branch name contains "/".
//
// The parse layer splits a tree URL at the first slash after /tree/; for a
// slash-named ref that boundary is wrong and cannot be fixed syntactically —
// it lives in the repository's ref list. The fast path (the naive join's
// pack.toml exists) is byte-identical to the old behavior. On a miss for a
// tree/blob source, the boundary is recovered from the cache's own refs
// (remote-tracking branches and tags — no network), preferring the longest
// matching ref exactly as GitHub's own tree URLs resolve. When no boundary
// yields a pack.toml, the refusal names the ambiguity and the "#ref" escape
// (`…//<subpath>#<ref>`) instead of resolving to a path that does not exist.
func cachedIncludeDir(source, cacheDir string) (string, error) {
	parsed := remotesource.Parse(source)
	naive := cacheDir
	if parsed.Subpath != "" {
		naive = filepath.Join(cacheDir, parsed.Subpath)
	}
	if parsed.GitHubMode == "" {
		return naive, nil
	}
	if _, err := os.Stat(filepath.Join(naive, "pack.toml")); err == nil {
		return naive, nil
	}

	afterTree := parsed.Ref
	if parsed.Subpath != "" {
		afterTree += "/" + parsed.Subpath
	}
	if refs, err := includeCacheRefs(cacheDir); err == nil {
		if ref, subpath, ok := remotesource.ResolveTreeRefAgainst(refs, afterTree); ok {
			resolved := cacheDir
			if subpath != "" {
				resolved = filepath.Join(cacheDir, subpath)
			}
			if _, err := os.Stat(filepath.Join(resolved, "pack.toml")); err == nil {
				return resolved, nil
			}
			if ref != parsed.Ref {
				// Proven mis-split: the cache's refs say the ref is
				// slash-named, and neither boundary yields a pack. Refuse
				// with the remedy rather than resolving to a path that
				// cannot exist.
				return "", fmt.Errorf(
					"no pack.toml under %q for source %q: the ref is %q (slash-named), so the tree URL's ref/path boundary is ambiguous; spell it unambiguously with the fragment form (…//<subpath>#<ref>) or use a slash-free ref",
					naive, source, ref)
			}
		}
	}
	// No recovery evidence: keep the naive resolution so downstream
	// diagnostics (missing-cached-pack checks, load errors) report as they
	// always have.
	return naive, nil
}

// includeCacheRefs lists the branch and tag names a fetched cache already
// knows without touching the network.
func includeCacheRefs(cacheDir string) ([]string, error) {
	cmd := exec.Command("git", "-C", cacheDir, "show-ref")
	cmd.Env = git.SanitizedEnv()
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("listing cache refs: %w", err)
	}
	var refs []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		ref := fields[1]
		switch {
		case strings.HasPrefix(ref, "refs/tags/"):
			refs = append(refs, strings.TrimSuffix(strings.TrimPrefix(ref, "refs/tags/"), "^{}"))
		case strings.HasPrefix(ref, "refs/remotes/"):
			rest := strings.TrimPrefix(ref, "refs/remotes/")
			if _, name, ok := strings.Cut(rest, "/"); ok && name != "HEAD" {
				refs = append(refs, name)
			}
		case strings.HasPrefix(ref, "refs/heads/"):
			refs = append(refs, strings.TrimPrefix(ref, "refs/heads/"))
		}
	}
	return refs, nil
}
