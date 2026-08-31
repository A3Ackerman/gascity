package remotesource

import "testing"

// A GitHub tree URL cannot syntactically distinguish the ref from the leading
// subpath segments — only the repository's real ref list can. The resolver
// picks the longest ref (by whole segments) that prefixes the after-tree
// remainder, matching how GitHub's own tree URLs disambiguate.
func TestResolveTreeRefAgainst(t *testing.T) {
	refs := []string{
		"main",
		"release/v1.3.0",
		"interim-box-dog-close",
		"interim/box-dog-close",
		"interim/box-dog-close/nested",
	}
	tests := []struct {
		name      string
		afterTree string
		wantRef   string
		wantPath  string
		wantOK    bool
	}{
		{"plain branch with path", "main/examples/bd", "main", "examples/bd", true},
		{"plain branch no path", "main", "main", "", true},
		{"slash branch with path", "interim/box-dog-close/examples/bd", "interim/box-dog-close", "examples/bd", true},
		{"longest ref wins over shorter prefix", "interim/box-dog-close/nested/examples", "interim/box-dog-close/nested", "examples", true},
		{"slash branch exactly, no path", "interim/box-dog-close", "interim/box-dog-close", "", true},
		{"release-style slash tag", "release/v1.3.0/packs/foo", "release/v1.3.0", "packs/foo", true},
		{"no ref matches", "nosuch/branch/path", "", "", false},
		{"segment boundaries only, not string prefixes", "mainline/examples", "", "", false},
		{"empty after-tree", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref, path, ok := ResolveTreeRefAgainst(refs, tt.afterTree)
			if ok != tt.wantOK || ref != tt.wantRef || path != tt.wantPath {
				t.Errorf("ResolveTreeRefAgainst(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tt.afterTree, ref, path, ok, tt.wantRef, tt.wantPath, tt.wantOK)
			}
		})
	}
}

// A commit sha in the ref position resolves without appearing in the ref
// list: shas are what pinned tree sources carry after normalization.
func TestResolveTreeRefAgainstAcceptsCommitSHA(t *testing.T) {
	ref, path, ok := ResolveTreeRefAgainst([]string{"main"}, "abcf2b63d99f4a2e8c11b7e5d0aa93c4f2e6b781/examples/bd")
	if !ok || ref != "abcf2b63d99f4a2e8c11b7e5d0aa93c4f2e6b781" || path != "examples/bd" {
		t.Errorf("sha resolution = (%q, %q, %v), want the sha + examples/bd", ref, path, ok)
	}
}
