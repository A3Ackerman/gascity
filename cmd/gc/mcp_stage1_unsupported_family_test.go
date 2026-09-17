package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/config"
)

// cityWithCityScopeMCPCatalog builds a city whose MCP catalog is declared at
// city scope, so every agent's effective MCP set is non-empty. The city agent
// runs on a supported provider family (claude); extra agents are appended
// as given.
func cityWithCityScopeMCPCatalog(t *testing.T, providers map[string]config.ProviderSpec, extra ...config.Agent) (string, *config.City) {
	t.Helper()
	cityPath := t.TempDir()
	rigPath := t.TempDir()
	writeMCPSource(t, filepath.Join(cityPath, "mcp", "notes.toml"), `
name = "notes"
command = "uvx"
args = ["notes-mcp"]
`)
	cfg := &config.City{
		PackMCPDir: filepath.Join(cityPath, "mcp"),
		Session:    config.SessionConfig{Provider: "tmux"},
		Providers:  providers,
		Rigs:       []config.Rig{{Name: "app", Path: rigPath}},
		Agents: append([]config.Agent{
			{Name: "mayor", Scope: "city", Provider: "claude"},
		}, extra...),
	}
	return cityPath, cfg
}

func requireProjectedNotesServer(t *testing.T, cityPath string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cityPath, ".mcp.json"))
	if err != nil {
		t.Fatalf("supported agent's stage-1 target was not written: %v", err)
	}
	if !strings.Contains(string(data), `"notes"`) {
		t.Fatalf(".mcp.json missing the city-scope server:\n%s", data)
	}
}

// Control: the city-scope catalog projects cleanly when every agent is on a
// supported provider family.
func TestRunStage1MCPProjectionCityScopeCatalogSupportedFamilyOnly(t *testing.T) {
	cityPath, cfg := cityWithCityScopeMCPCatalog(t, builtinProviderAliasesForTest("claude"))

	var stderr bytes.Buffer
	if err := runStage1MCPProjection(cityPath, cfg, stubLookPath, &stderr); err != nil {
		t.Fatalf("runStage1MCPProjection: %v", err)
	}
	requireProjectedNotesServer(t, cityPath)
}

// runStage1MCPProjection is the "projecting_mcp" step of
// prepareCityForSupervisor and a hard gate in `gc start`; its error fails city
// init and the supervisor retries with backoff. One agent whose provider
// family has no MCP projection must not take the whole city down: the
// supported agents are still projected and the unprojectable agent is named
// on stderr. (The tick-time path already treats the same error per agent:
// buildDesiredState logs it and skips that agent.)
func TestRunStage1MCPProjectionUnsupportedFamilyDoesNotFailCity(t *testing.T) {
	custom := builtinProviderAliasesForTest("claude")
	custom["acme"] = config.ProviderSpec{Command: "acme-agent", PromptMode: "none"}

	cases := []struct {
		name      string
		providers map[string]config.ProviderSpec
		provider  string
	}{
		{name: "builtin omp", providers: builtinProviderAliasesForTest("claude", "omp"), provider: "omp"},
		{name: "custom provider without a builtin base", providers: custom, provider: "acme"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cityPath, cfg := cityWithCityScopeMCPCatalog(t, tc.providers,
				config.Agent{Name: "worker", Scope: "rig", Dir: "app", Provider: tc.provider})

			var stderr bytes.Buffer
			if err := runStage1MCPProjection(cityPath, cfg, stubLookPath, &stderr); err != nil {
				t.Fatalf("one agent on provider %q failed stage-1 MCP projection for the whole city: %v", tc.provider, err)
			}
			requireProjectedNotesServer(t, cityPath)
			if !strings.Contains(stderr.String(), "app/worker") {
				t.Fatalf("unprojectable agent app/worker was skipped silently; stderr:\n%s", stderr.String())
			}
		})
	}
}
