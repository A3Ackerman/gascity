package remotesource

import "strings"

// ResolveTreeRefAgainst splits a tree URL's after-ref remainder into (ref,
// subpath) using the repository's real ref list. A tree URL cannot carry the
// boundary syntactically — a branch name may itself contain "/" — so the only
// correct split is the one GitHub's own tree URLs use: the longest ref, by
// whole path segments, that prefixes the remainder. A 40-hex first segment is
// accepted as a commit sha without consulting refs, since pinned tree sources
// carry shas that appear in no ref list. Returns ok=false when nothing
// matches; callers refuse loudly there instead of guessing a boundary.
func ResolveTreeRefAgainst(refs []string, afterTree string) (ref, subpath string, ok bool) {
	afterTree = strings.Trim(strings.TrimSpace(afterTree), "/")
	if afterTree == "" {
		return "", "", false
	}
	if first, rest, _ := strings.Cut(afterTree, "/"); isCommitSHA(first) {
		return first, rest, true
	}
	best := ""
	for _, candidate := range refs {
		candidate = strings.Trim(strings.TrimSpace(candidate), "/")
		if candidate == "" || len(candidate) < len(best) {
			continue
		}
		if afterTree == candidate || strings.HasPrefix(afterTree, candidate+"/") {
			best = candidate
		}
	}
	if best == "" {
		return "", "", false
	}
	return best, strings.Trim(strings.TrimPrefix(afterTree, best), "/"), true
}

func isCommitSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
