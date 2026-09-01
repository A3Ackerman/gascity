package storebinding

import (
	"testing"

	"github.com/gastownhall/gascity/internal/coordclass"
)

// TestClassSetMintClassIsTheLeadClass pins the one rule every binding engine
// derives its mint prefix from: a binding mints under its LEAD class's reserved
// prefix. Graph leads whenever it is present — which keeps every deployed
// whole-split city minting gcg exactly as it does today — and a binding that
// serves exactly one infrastructure class leads with that class, so a
// messaging-only binding mints gcm.
func TestClassSetMintClassIsTheLeadClass(t *testing.T) {
	whole := mustClassSet(t, coordclass.ClassGraph, coordclass.ClassSessions,
		coordclass.ClassMessaging, coordclass.ClassOrders, coordclass.ClassNudges)

	for name, tc := range map[string]struct {
		classes ClassSet
		want    coordclass.Class
	}{
		"the whole split leads with graph":         {whole, coordclass.ClassGraph},
		"graph alone leads with graph":             {mustClassSet(t, coordclass.ClassGraph), coordclass.ClassGraph},
		"graph beside work still leads with graph": {mustClassSet(t, coordclass.ClassWork, coordclass.ClassGraph), coordclass.ClassGraph},
		"messaging alone leads with messaging":     {mustClassSet(t, coordclass.ClassMessaging), coordclass.ClassMessaging},
		"orders alone leads with orders":           {mustClassSet(t, coordclass.ClassOrders), coordclass.ClassOrders},
		"sessions alone leads with sessions":       {mustClassSet(t, coordclass.ClassSessions), coordclass.ClassSessions},
		"nudges alone leads with nudges":           {mustClassSet(t, coordclass.ClassNudges), coordclass.ClassNudges},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := tc.classes.MintClass()
			if !ok {
				t.Fatalf("MintClass(%v) reported no lead class", tc.classes.Classes())
			}
			if got != tc.want {
				t.Fatalf("MintClass(%v) = %s, want %s", tc.classes.Classes(), got, tc.want)
			}
		})
	}
}

// TestClassSetMintClassRefusesSetsWithNoSinglePrefix is the other half of the
// rule: a set that names no reserved prefix, or would need to choose between
// several, has no lead class. Refusing here — rather than picking one — is what
// keeps a multi-class binding that leaves graph behind from minting one class's
// ids for another's beads.
func TestClassSetMintClassRefusesSetsWithNoSinglePrefix(t *testing.T) {
	for name, classes := range map[string]ClassSet{
		"the empty set":                            {},
		"work alone, which has no prefix":          mustClassSet(t, coordclass.ClassWork),
		"two infrastructure classes without graph": mustClassSet(t, coordclass.ClassMessaging, coordclass.ClassOrders),
		"work beside one infrastructure class":     mustClassSet(t, coordclass.ClassWork, coordclass.ClassMessaging),
	} {
		t.Run(name, func(t *testing.T) {
			if got, ok := classes.MintClass(); ok {
				t.Fatalf("MintClass(%v) = %s, want no lead class", classes.Classes(), got)
			}
		})
	}
}

func mustClassSet(t *testing.T, classes ...coordclass.Class) ClassSet {
	t.Helper()
	set, err := NewClassSet(classes...)
	if err != nil {
		t.Fatalf("NewClassSet(%v): %v", classes, err)
	}
	return set
}
