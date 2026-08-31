package packman

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gastownhall/gascity/internal/remotesource"
)

// resolveCachedPackDir returns the directory inside cachePath that holds the
// source's pack.toml, recovering the ref/subpath boundary for GitHub tree and
// blob sources whose branch name contains "/".
//
// The parse layer splits a tree URL at the first slash after /tree/, which is
// wrong for slash-named refs and cannot be fixed there: the boundary is not
// syntactic, it lives in the repository's ref list. The fast path — the naive
// split's pack.toml exists — is byte-identical to the old join, so every
// working source is untouched. On a miss for a tree/blob source, the boundary
// is recovered from the cache's own refs (remote-tracking branches and tags —
// present in any fetched cache, no network), preferring the longest matching
// ref exactly as GitHub's own tree URLs resolve. When no boundary yields a
// pack.toml, the refusal names the ambiguity and the "#ref" escape instead of
// reporting a mis-split path as merely missing.
func resolveCachedPackDir(source, cachePath string) (string, error) {
	parsed := remotesource.Parse(source)
	naive := cachePath
	if parsed.Subpath != "" {
		naive = filepath.Join(cachePath, parsed.Subpath)
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
	if refs, refsErr := cacheLocalRefs(cachePath); refsErr == nil {
		if ref, subpath, ok := remotesource.ResolveTreeRefAgainst(refs, afterTree); ok {
			resolved := cachePath
			if subpath != "" {
				resolved = filepath.Join(cachePath, subpath)
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

// cacheLocalRefs lists the branch and tag names a fetched cache already
// knows, without touching the network: remote-tracking branches under
// refs/remotes/<remote>/ (the remote name segment stripped) and tags.
func cacheLocalRefs(cachePath string) ([]string, error) {
	out, err := runGit(cachePath, "show-ref")
	if err != nil {
		return nil, err
	}
	var refs []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
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
