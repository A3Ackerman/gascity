package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gastownhall/gascity/internal/config"
	"github.com/gastownhall/gascity/internal/fsys"
)

// TestResolveSessionTemplate_MissingDeclaredPromptIsNotSilent drives the
// session boot path (resolveTemplate, cmd/gc/template_resolve.go — the
// renderPrompt call at Step 9) for an agent whose declared prompt_template
// file does not exist. On stock the render returns "" without a word,
// resolveTemplate reads "" as "this agent has no prompt"
// (includePrimeInstruction := !hasHooks && prompt == ""), ships the bare
// beacon plus the `gc prime` instruction, and the seat ends up on
// defaultPrimePrompt. TemplateParams carries no diagnostic field and no error
// comes back, so the only surfaces a fix could use are the error return and
// agentBuildParams.stderr. The assertion is the weakest form any reasonable
// fix satisfies: some diagnostic on one of those surfaces names the missing
// path.
func TestResolveSessionTemplate_MissingDeclaredPromptIsNotSilent(t *testing.T) {
	newParams := func(cityPath string, stderr io.Writer) *agentBuildParams {
		return &agentBuildParams{
			fs:              fsys.OSFS{},
			cityName:        "bright-lights",
			cityPath:        cityPath,
			workspace:       &config.Workspace{Name: "bright-lights", Provider: "opencode"},
			providers:       config.BuiltinProviders(),
			lookPath:        func(string) (string, error) { return "/usr/bin/opencode", nil },
			beaconTime:      testBeaconTime,
			sessionTemplate: "",
			beadNames:       make(map[string]string),
			stderr:          stderr,
		}
	}

	t.Run("declared but missing", func(t *testing.T) {
		cityPath := t.TempDir()
		const templatePath = "prompts/crew.template.md"
		if _, err := os.Stat(filepath.Join(cityPath, templatePath)); !os.IsNotExist(err) {
			t.Fatalf("fixture precondition: %s must not exist under %s (stat err=%v)", templatePath, cityPath, err)
		}
		var stderr strings.Builder
		agent := &config.Agent{
			Name:           "navani",
			PromptTemplate: templatePath,
			Provider:       "opencode",
		}

		tp, err := resolveTemplate(newParams(cityPath, &stderr), agent, agent.QualifiedName(), nil)

		// Either surface is acceptable: a returned error, or a stderr line.
		diagnostics := stderr.String()
		if err != nil {
			diagnostics += "\nerror: " + err.Error()
		}
		if strings.TrimSpace(diagnostics) == "" {
			t.Fatalf("agent %q declares prompt_template %q, the file is missing, and the boot path said nothing (err=nil, stderr empty); the session will ship prompt=%q and the seat will boot on the built-in default prompt with no signal", agent.QualifiedName(), templatePath, tp.Prompt)
		}
		if !strings.Contains(diagnostics, templatePath) {
			t.Fatalf("boot-path diagnostics = %q, want one that names the missing template path %q", diagnostics, templatePath)
		}
	})

	t.Run("control: no prompt_template declared", func(t *testing.T) {
		cityPath := t.TempDir()
		var stderr strings.Builder
		agent := &config.Agent{
			Name:     "navani",
			Provider: "opencode",
		}

		if _, err := resolveTemplate(newParams(cityPath, &stderr), agent, agent.QualifiedName(), nil); err != nil {
			t.Fatalf("resolveTemplate: %v", err)
		}
		if stderr.Len() != 0 {
			t.Fatalf("an agent that declares no prompt_template must resolve silently; stderr = %q", stderr.String())
		}
	})
}
