package orders

import (
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/fsys"
)

// TestApplyOverridesAppliesEntriesAfterAMissingName pins the contract the
// controller and API callers rely on when they log an override error and
// continue: an override that names an order absent from the scanned set is
// reported, and only that entry is skipped. Every other override still applies.
func TestApplyOverridesAppliesEntriesAfterAMissingName(t *testing.T) {
	t.Parallel()

	off := boolPtr(false)

	t.Run("missing name between two resolvable overrides", func(t *testing.T) {
		t.Parallel()
		aa := []Order{{Name: "order-a"}, {Name: "order-b"}, {Name: "order-c"}}
		err := ApplyOverrides(aa, []Override{
			{Name: "order-a", Enabled: off},
			{Name: "order-x", Enabled: off}, // order dropped or renamed by a pack update
			{Name: "order-c", Enabled: off},
		})
		if err == nil || !strings.Contains(err.Error(), `"order-x"`) {
			t.Errorf("ApplyOverrides error = %v, want an error naming %q", err, "order-x")
		}
		assertEnabled(t, aa, map[string]bool{"order-a": false, "order-b": true, "order-c": false})
	})

	t.Run("control: every override resolves", func(t *testing.T) {
		t.Parallel()
		aa := []Order{{Name: "order-a"}, {Name: "order-b"}, {Name: "order-c"}}
		err := ApplyOverrides(aa, []Override{
			{Name: "order-a", Enabled: off},
			{Name: "order-c", Enabled: off},
		})
		if err != nil {
			t.Fatalf("ApplyOverrides: %v", err)
		}
		assertEnabled(t, aa, map[string]bool{"order-a": false, "order-b": true, "order-c": false})
	})

	t.Run("override left behind for an order moved to skip", func(t *testing.T) {
		t.Parallel()
		fs := fsys.NewFake()
		for _, name := range []string{"order-a", "order-b", "order-c"} {
			fs.Files["/pack/orders/"+name+".toml"] = []byte(`
[order]
formula = "mol-` + name + `"
trigger = "cooldown"
interval = "1h"
`)
		}
		aa, err := ScanRoots(fs, []ScanRoot{{Dir: "/pack/orders", FormulaLayer: "/pack/formulas"}}, []string{"order-b"})
		if err != nil {
			t.Fatalf("ScanRoots: %v", err)
		}
		err = ApplyOverrides(aa, []Override{
			{Name: "order-b", Interval: strPtr("2h")}, // skipped orders never reach ApplyOverrides
			{Name: "order-c", Enabled: off},
		})
		if err == nil || !strings.Contains(err.Error(), `"order-b"`) {
			t.Errorf("ApplyOverrides error = %v, want an error naming %q", err, "order-b")
		}
		assertEnabled(t, aa, map[string]bool{"order-a": true, "order-c": false})
	})
}

// assertEnabled checks IsEnabled for each named order in aa and fails when a
// named order is missing from the slice.
func assertEnabled(t *testing.T, aa []Order, want map[string]bool) {
	t.Helper()
	seen := make(map[string]bool, len(aa))
	for i := range aa {
		wantEnabled, ok := want[aa[i].Name]
		if !ok {
			t.Errorf("unexpected order %q in scanned set", aa[i].Name)
			continue
		}
		seen[aa[i].Name] = true
		if got := aa[i].IsEnabled(); got != wantEnabled {
			t.Errorf("order %q IsEnabled() = %v, want %v", aa[i].Name, got, wantEnabled)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("order %q missing from scanned set", name)
		}
	}
}
