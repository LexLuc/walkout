package claudecode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWalkoutClaudePluginPackagesEmergencyContinueCommands pins the Claude
// Code packaging contract: a marketplace manifest that lists the shared
// walkout plugin directory, a Claude plugin manifest that coexists with the
// Codex one, and the two self-authorizing slash commands validated by the
// interactive probe. The lifecycle hooks the commands rely on are asserted
// separately in TestWalkoutClaudePluginBundlesHealthHooks.
func TestWalkoutClaudePluginPackagesEmergencyContinueCommands(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..", "..")

	// The marketplace lists the walkout plugin at the shared plugin dir.
	var market struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name   string `json:"name"`
			Source string `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(readFileBytes(t, filepath.Join(repoRoot, ".claude-plugin", "marketplace.json")), &market); err != nil {
		t.Fatalf("decode marketplace manifest: %v", err)
	}
	var listed bool
	for _, p := range market.Plugins {
		if p.Name == "walkout" {
			listed = true
			if p.Source != "./plugins/walkout" {
				t.Errorf("walkout source = %q, want ./plugins/walkout", p.Source)
			}
		}
	}
	if !listed {
		t.Fatal("marketplace does not list the walkout plugin")
	}

	pluginRoot := filepath.Join(repoRoot, "plugins", "walkout")

	// The Claude plugin manifest sits beside the Codex manifest in one dir.
	var manifest struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(readFileBytes(t, filepath.Join(pluginRoot, ".claude-plugin", "plugin.json")), &manifest); err != nil {
		t.Fatalf("decode claude plugin manifest: %v", err)
	}
	if manifest.Name != "walkout" {
		t.Fatalf("claude plugin name = %q, want walkout", manifest.Name)
	}

	// continue: model invocation disabled, reason forwarded, turn constrained.
	continueCmd := string(readFileBytes(t, filepath.Join(pluginRoot, "commands", "continue.md")))
	for _, required := range []string{
		"disable-model-invocation: true",
		"argument-hint: '[reason]'",
		"${CLAUDE_PLUGIN_ROOT}/bin/walkout-ctl.exe",
		"emergency-continue -reason \"$ARGUMENTS\"",
		"one short line",
		"do not read or modify any files",
	} {
		if !strings.Contains(continueCmd, required) {
			t.Errorf("continue command omits %q", required)
		}
	}
	if strings.Contains(continueCmd, "[TODO:") {
		t.Error("continue command retains a scaffold placeholder")
	}

	// done: the primary in-host confirm entry. It forwards one walkout-ctl
	// verb, disables model invocation, and constrains the model turn to a
	// one-line acknowledgement.
	recoveryEntries := map[string]string{
		"done.md": "done",
	}
	for file, verb := range recoveryEntries {
		content := string(readFileBytes(t, filepath.Join(pluginRoot, "commands", file)))
		for _, required := range []string{
			"disable-model-invocation: true",
			"${CLAUDE_PLUGIN_ROOT}/bin/walkout-ctl.exe",
			"one short line",
			"do not read or modify any files",
		} {
			if !strings.Contains(content, required) {
				t.Errorf("%s omits %q", file, required)
			}
		}
		if !strings.Contains(content, "walkout-ctl.exe\" "+verb) {
			t.Errorf("%s does not invoke the %q verb", file, verb)
		}
	}

	// status: read-only, model invocation disabled, output relayed verbatim so
	// every call renders the same way instead of a model-authored summary.
	statusCmd := string(readFileBytes(t, filepath.Join(pluginRoot, "commands", "status.md")))
	for _, required := range []string{
		"disable-model-invocation: true",
		"${CLAUDE_PLUGIN_ROOT}/bin/walkout-ctl.exe",
		"status",
		"verbatim",
	} {
		if !strings.Contains(statusCmd, required) {
			t.Errorf("status command omits %q", required)
		}
	}
}

// TestWalkoutClaudePluginVersionMatchesMarketplace pins that the plugin
// manifest and the marketplace catalog advertise the same version: `claude
// plugin update` decides from the catalog entry, so a manifest-only bump
// leaves installed users on the old build (verified on 2.1.281).
func TestWalkoutClaudePluginVersionMatchesMarketplace(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..", "..")
	var manifest struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(readFileBytes(t, filepath.Join(repoRoot, "plugins", "walkout", ".claude-plugin", "plugin.json")), &manifest); err != nil {
		t.Fatalf("decode plugin manifest: %v", err)
	}
	var marketplace struct {
		Plugins []struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(readFileBytes(t, filepath.Join(repoRoot, ".claude-plugin", "marketplace.json")), &marketplace); err != nil {
		t.Fatalf("decode marketplace: %v", err)
	}
	found := false
	for _, entry := range marketplace.Plugins {
		if entry.Name != manifest.Name {
			continue
		}
		found = true
		if entry.Version != manifest.Version {
			t.Fatalf("marketplace advertises %s %q but the plugin manifest is %q; bump both together", entry.Name, entry.Version, manifest.Version)
		}
	}
	if !found {
		t.Fatalf("marketplace has no entry for plugin %q", manifest.Name)
	}
	if manifest.Version == "" {
		t.Fatal("plugin manifest version is empty")
	}
}

// TestWalkoutClaudePluginBundlesHealthHooks pins that the plugin ships its
// lifecycle hooks so installing it wires the guard automatically. The hook's
// host version is best-effort: it is passed from $CLAUDE_CODE_VERSION and the
// translator defaults a missing value to "unknown", so the guard still works if
// the host does not expand that variable (confirmed by the real-install probe).
func TestWalkoutClaudePluginBundlesHealthHooks(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "..", "plugins", "walkout", "hooks", "hooks.json")
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
				Timeout int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(readFileBytes(t, path), &doc); err != nil {
		t.Fatalf("decode hooks.json: %v", err)
	}

	var commands []string
	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop", "SessionEnd"} {
		groups, ok := doc.Hooks[event]
		if !ok || len(groups) == 0 {
			t.Errorf("hooks.json omits the %s event", event)
			continue
		}
		for _, g := range groups {
			for _, h := range g.Hooks {
				if h.Type != "command" {
					t.Errorf("%s hook type = %q, want command", event, h.Type)
				}
				commands = append(commands, h.Command)
			}
		}
	}
	if len(commands) == 0 {
		t.Fatal("hooks.json declares no command hooks")
	}
	for _, cmd := range commands {
		for _, required := range []string{
			"${CLAUDE_PLUGIN_ROOT}/bin/walkout-hook.exe",
			"-provider claude-code",
			"$CLAUDE_CODE_VERSION",
		} {
			if !strings.Contains(cmd, required) {
				t.Errorf("hook command %q omits %q", cmd, required)
			}
		}
	}
}

func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}
