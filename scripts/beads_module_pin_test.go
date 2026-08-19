package scripts_test

import (
	"regexp"
	"testing"
)

// TestBeadsModulePin anchors go.mod's beads requirement, the way
// TestDoltVersionPins anchors Dolt's. The native store IS the beads library
// linked into gc, so this one line decides the highest Dolt schema version a gc
// binary can open. Our city databases are migrated by bd built from this same
// revision; a gc pinned below it trips beads' schema-skew gate and falls back
// to the exec store rather than failing, so the town runs degraded with nothing
// red anywhere. A gc pinned above it is the same hazard mirrored — it migrates
// a city DB past what every other machine's bd knows.
//
// Neither direction is a go.mod edit. Moving this pin means redeploying bd
// across the fleet in the same window, so the pin moves here, in CARRY.md's
// "Beads pin" table, and on every machine, together.
//
// Note this is a different axis from deps.env's BD_VERSION, which pins the bd
// *release tarball* CI and Docker install and can only name a published tag.
// The two are deliberately independent; TestBDVersionPins owns that one.
func TestBeadsModulePin(t *testing.T) {
	// v1.1.1-0.20260716185344-67652d8b5caf is commit 67652d8b5caf on
	// gastownhall/beads main (2026-07-16), the first revision carrying schema
	// v54 (0054_add_lease_columns) and the revision every bd in this town is
	// built from. It is a pseudo-version rather than a tag because no released
	// tag carries v54: v1.1.0 and v1.1.1 both stop at v53.
	const beadsFleetPin = "v1.1.1-0.20260716185344-67652d8b5caf"

	gomod := readFile(t, repoRoot(t), "go.mod")

	// Match every version this go.mod associates with the beads module —
	// require and replace alike — so a stale requirement cannot hide beside a
	// correct one, and a substring match cannot be satisfied by a comment.
	re := regexp.MustCompile(`(?m)^\s*(?:replace\s+|require\s+)?github\.com/steveyegge/beads\s+(v\S+)`)
	matches := re.FindAllStringSubmatch(gomod, -1)
	if len(matches) == 0 {
		t.Fatal("go.mod names no version for github.com/steveyegge/beads")
	}
	for _, m := range matches {
		if m[1] != beadsFleetPin {
			t.Errorf("go.mod pins github.com/steveyegge/beads %s; this town's bd is built from %s. If the pin is meant to move, move beadsFleetPin, CARRY.md's beads pin table, and every machine's bd together.",
				m[1], beadsFleetPin)
		}
	}
}
