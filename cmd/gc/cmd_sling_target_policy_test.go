package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/beads"
)

func TestCmdSlingInlineOwnedDefaultTargetRejectsBeforeBeadCreate(t *testing.T) {
	cityDir := setupCmdSlingBeadExistsFixture(t)
	f, err := os.OpenFile(filepath.Join(cityDir, "city.toml"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("\n[convoys]\nrequire_owned_target = true\nforbid_default_target = true\n")
	_ = f.Close()

	store, err := openStoreAtForCity(filepath.Join(cityDir, "frontend"), cityDir)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.List(beads.ListQuery{AllowScan: true, IncludeClosed: true})
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := cmdSlingWithConvoyTarget([]string{"frontend/worker", "write docs"}, false, false, false, "", nil, "", false, true, "main", false, "", false, false, false, "", "", &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), `target "main" is the owning rig default branch`) {
		t.Fatalf("code=%d stderr=%q, want default-target rejection", code, stderr.String())
	}
	after, err := store.List(beads.ListQuery{AllowScan: true, IncludeClosed: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("beads after rejected inline sling = %d, before = %d; want no side effect", len(after), len(before))
	}
}

func TestCmdSlingOwnedNoConvoyAllowsMissingTarget(t *testing.T) {
	cityDir := setupCmdSlingBeadExistsFixture(t)
	f, err := os.OpenFile(filepath.Join(cityDir, "city.toml"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("\n[convoys]\nrequire_owned_target = true\nforbid_default_target = true\n")
	_ = f.Close()
	var stdout, stderr bytes.Buffer
	code := cmdSlingWithConvoyTarget([]string{"frontend/worker", "write docs"}, false, false, false, "", nil, "", true, true, "", false, "", false, false, false, "", "", &stdout, &stderr)
	if strings.Contains(stderr.String(), "owned convoy requires an explicit target") || strings.Contains(stderr.String(), "convoy_target_policy") {
		t.Fatalf("no-convoy owned sling hit convoy policy: code=%d stderr=%q", code, stderr.String())
	}
}
