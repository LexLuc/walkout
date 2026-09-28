package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWalkoutPluginExposesAnExplicitContinueSkill(t *testing.T) {
	t.Parallel()

	pluginRoot := filepath.Join("..", "..", "..", "plugins", "walkout-codex")
	manifestBytes, err := os.ReadFile(filepath.Join(pluginRoot, ".codex-plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("read plugin manifest: %v", err)
	}
	var manifest struct {
		Name   string `json:"name"`
		Skills string `json:"skills"`
		Hooks  string `json:"hooks"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("decode plugin manifest: %v", err)
	}
	if manifest.Name != "walkout" || manifest.Skills != "./skills/" {
		t.Fatalf("plugin identity = %#v", manifest)
	}
	if manifest.Hooks != "./.codex/hooks.json" {
		t.Fatalf("plugin hooks = %q, want ./.codex/hooks.json", manifest.Hooks)
	}

	skillBytes, err := os.ReadFile(filepath.Join(pluginRoot, "skills", "continue", "SKILL.md"))
	if err != nil {
		t.Fatalf("read continue skill: %v", err)
	}
	skill := string(skillBytes)
	for _, required := range []string{
		"name: continue",
		"Respond with one short sentence",
		"Do not inspect files, call tools, or continue prior work",
	} {
		if !strings.Contains(skill, required) {
			t.Errorf("continue skill omits %q", required)
		}
	}
	if strings.Contains(skill, "[TODO:") {
		t.Error("continue skill retains a scaffold placeholder")
	}

	agentBytes, err := os.ReadFile(filepath.Join(pluginRoot, "skills", "continue", "agents", "openai.yaml"))
	if err != nil {
		t.Fatalf("read continue skill UI metadata: %v", err)
	}
	agentMetadata := string(agentBytes)
	if !strings.Contains(agentMetadata, `default_prompt: "$walkout:continue"`) {
		t.Errorf("openai.yaml starter is not the exact namespaced control marker: %s", agentMetadata)
	}
	if !strings.Contains(agentMetadata, "allow_implicit_invocation: false") {
		t.Errorf("continue control skill allows implicit invocation: %s", agentMetadata)
	}
}

// TestWalkoutPluginExposesTheRecoveryLoopSkills pins the in-conversation
// confirm entry: done is an explicit control marker with the same
// anti-implicit-invocation posture as continue, and the skill constrains the
// model turn to a short acknowledgement without tool use.
func TestWalkoutPluginExposesTheRecoveryLoopSkills(t *testing.T) {
	t.Parallel()

	pluginRoot := filepath.Join("..", "..", "..", "plugins", "walkout-codex")
	for _, name := range []string{"done"} {
		skill := string(readCodexPluginFile(t, filepath.Join(pluginRoot, "skills", name, "SKILL.md")))
		for _, required := range []string{
			"name: " + name,
			"one short sentence",
			"Do not inspect files, call tools, or continue prior work",
		} {
			if !strings.Contains(skill, required) {
				t.Errorf("%s skill omits %q", name, required)
			}
		}
		if strings.Contains(skill, "[TODO:") {
			t.Errorf("%s skill retains a scaffold placeholder", name)
		}

		metadata := string(readCodexPluginFile(t, filepath.Join(pluginRoot, "skills", name, "agents", "openai.yaml")))
		if !strings.Contains(metadata, `default_prompt: "$walkout:`+name+`"`) {
			t.Errorf("%s openai.yaml starter is not the exact namespaced control marker: %s", name, metadata)
		}
		if !strings.Contains(metadata, "allow_implicit_invocation: false") {
			t.Errorf("%s control skill allows implicit invocation: %s", name, metadata)
		}
	}
}

// TestWalkoutPluginExposesAStatusSkill pins the read-only status entry that
// mirrors Claude Code's /walkout:status: the model runs walkout-ctl status
// outside the Codex sandbox (the sandbox cannot open the daemon's named pipe)
// and relays the output verbatim, without doing anything else.
func TestWalkoutPluginExposesAStatusSkill(t *testing.T) {
	t.Parallel()

	pluginRoot := filepath.Join("..", "..", "..", "plugins", "walkout-codex")
	skill := string(readCodexPluginFile(t, filepath.Join(pluginRoot, "skills", "status", "SKILL.md")))
	for _, required := range []string{
		"name: status",
		"walkout-ctl status",
		"escalated permissions",
		"verbatim",
		"do not take any other action",
	} {
		if !strings.Contains(skill, required) {
			t.Errorf("status skill omits %q", required)
		}
	}
	metadata := string(readCodexPluginFile(t, filepath.Join(pluginRoot, "skills", "status", "agents", "openai.yaml")))
	if !strings.Contains(metadata, `default_prompt: "$walkout:status"`) {
		t.Errorf("status openai.yaml starter is not the exact namespaced entry: %s", metadata)
	}
	if !strings.Contains(metadata, "allow_implicit_invocation: false") {
		t.Errorf("status skill allows implicit invocation: %s", metadata)
	}
}

func TestWalkoutPluginBundlesCodexHealthHooks(t *testing.T) {
	t.Parallel()

	pluginRoot := filepath.Join("..", "..", "..", "plugins", "walkout-codex")
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type           string `json:"type"`
				Command        string `json:"command"`
				CommandWindows string `json:"commandWindows"`
				Timeout        int    `json:"timeout"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(readCodexPluginFile(t, filepath.Join(pluginRoot, ".codex", "hooks.json")), &doc); err != nil {
		t.Fatalf("decode .codex/hooks.json: %v", err)
	}

	wantEvents := []string{"SessionStart", "UserPromptSubmit", "SessionEnd"}
	if len(doc.Hooks) != len(wantEvents) {
		t.Errorf("hook event count = %d, want %d", len(doc.Hooks), len(wantEvents))
	}
	for _, event := range wantEvents {
		groups := doc.Hooks[event]
		if len(groups) == 0 {
			t.Errorf(".codex/hooks.json omits %s", event)
			continue
		}
		for _, group := range groups {
			if len(group.Hooks) == 0 {
				t.Errorf("%s declares an empty hook group", event)
			}
			for _, hook := range group.Hooks {
				if hook.Type != "command" {
					t.Errorf("%s hook type = %q, want command", event, hook.Type)
				}
				if event == "SessionEnd" && (hook.Timeout < 1 || hook.Timeout > 3) {
					t.Errorf("SessionEnd timeout = %d, want 1..3 seconds", hook.Timeout)
				}
				// The version source is passed as the env var NAME and resolved
				// inside the always-PowerShell wrapper, so the two command strings
				// must be identical and must not depend on the invoking shell
				// expanding a bare $CODEX_MANAGED_PACKAGE_ROOT.
				if hook.Command != hook.CommandWindows {
					t.Errorf("%s command and commandWindows differ; they must be shell-agnostic and identical", event)
				}
				for field, command := range map[string]string{
					"command":        hook.Command,
					"commandWindows": hook.CommandWindows,
				} {
					for _, required := range []string{
						"pwsh -NoProfile -File",
						"${PLUGIN_ROOT}/hooks/run-hook.ps1",
						"${PLUGIN_ROOT}/bin/walkout-hook.exe",
						"-Provider codex",
						"-HostPackageEnv CODEX_MANAGED_PACKAGE_ROOT",
					} {
						if !strings.Contains(command, required) {
							t.Errorf("%s %s %q omits %q", event, field, command, required)
						}
					}
					if strings.Contains(command, "CODEX_MANAGED_PACKAGE_ROOT/package.json") {
						t.Errorf("%s %s expands the package path in the shell instead of passing the env var name: %q", event, field, command)
					}
					if strings.Contains(command, `\`) {
						t.Errorf("%s %s contains a backslash path: %q", event, field, command)
					}
					if strings.Contains(command, " -pipe ") {
						t.Errorf("%s %s overrides the default SID pipe: %q", event, field, command)
					}
				}
			}
		}
	}

	runner := string(readCodexPluginFile(t, filepath.Join(pluginRoot, "hooks", "run-hook.ps1")))
	for _, required := range []string{
		"$HostPackageEnv",
		"GetEnvironmentVariable($HostPackageEnv)",
		"ConvertFrom-Json",
		".version",
		"'-host-version'",
		"RedirectStandardInput = $true",
		"RedirectStandardOutput = $true",
	} {
		if !strings.Contains(runner, required) {
			t.Errorf("run-hook.ps1 omits %q", required)
		}
	}
	// Guard against the stdin/stdout pipe deadlock: the async output readers
	// must be created before the synchronous stdin write.
	readerAt := strings.Index(runner, "StandardOutput.ReadToEndAsync()")
	writeAt := strings.Index(runner, "StandardInput.Write(")
	if readerAt < 0 || writeAt < 0 {
		t.Fatalf("run-hook.ps1 missing stdout reader or stdin write")
	}
	if readerAt > writeAt {
		t.Error("run-hook.ps1 writes stdin before starting the async output readers; this can deadlock")
	}

	build := string(readCodexPluginFile(t, filepath.Join(pluginRoot, "build.ps1")))
	for _, required := range []string{"walkout-hook.exe", "./cmd/walkout-hook", "GOOS", "windows", "GOARCH", "amd64"} {
		if !strings.Contains(build, required) {
			t.Errorf("build.ps1 omits %q", required)
		}
	}
}

func TestWalkoutCodexMarketplaceAndTrustGuide(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..", "..")
	var market struct {
		Name    string `json:"name"`
		Plugins []struct {
			Name   string `json:"name"`
			Source struct {
				Source string `json:"source"`
				Path   string `json:"path"`
			} `json:"source"`
			Policy struct {
				Installation   string `json:"installation"`
				Authentication string `json:"authentication"`
			} `json:"policy"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(readCodexPluginFile(t, filepath.Join(repoRoot, ".agents", "plugins", "marketplace.json")), &market); err != nil {
		t.Fatalf("decode Codex marketplace: %v", err)
	}
	if market.Name != "lexicon" || len(market.Plugins) != 1 {
		t.Fatalf("marketplace identity = %#v", market)
	}
	plugin := market.Plugins[0]
	if plugin.Name != "walkout" || plugin.Source.Source != "local" || plugin.Source.Path != "./plugins/walkout-codex" {
		t.Errorf("marketplace plugin entry = %#v", plugin)
	}
	if plugin.Policy.Installation != "AVAILABLE" || plugin.Policy.Authentication != "ON_INSTALL" {
		t.Errorf("marketplace plugin policy = %#v", plugin.Policy)
	}

	guide := string(readCodexPluginFile(t, filepath.Join(repoRoot, "plugins", "walkout-codex", "README.md")))
	for _, required := range []string{
		"codex plugin marketplace add",
		"codex plugin add walkout@lexicon",
		"each event",
		"command hash",
		"re-authorize",
		"CODEX_MANAGED_PACKAGE_ROOT",
		`"unknown"`,
	} {
		if !strings.Contains(guide, required) {
			t.Errorf("Codex install guide omits %q", required)
		}
	}
}

func TestWalkoutCodexProbeCapturesOnlySanitizedEventVersion(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join("..", "..", "..")
	probeRoot := filepath.Join(repoRoot, "manual-probes", "codex-emergency-continue")
	startCodex := string(readCodexPluginFile(t, filepath.Join(probeRoot, "start-codex.ps1")))
	for _, required := range []string{"WALKOUT_EVENT_PROBE_OUTPUT", "health-events.jsonl"} {
		if !strings.Contains(startCodex, required) {
			t.Errorf("start-codex.ps1 omits %q", required)
		}
	}
	for _, scriptName := range []string{"prepare.ps1", "reset.ps1"} {
		script := string(readCodexPluginFile(t, filepath.Join(probeRoot, scriptName)))
		if !strings.Contains(script, "health-events.jsonl") {
			t.Errorf("%s does not clear the event metadata probe", scriptName)
		}
	}
	guide := string(readCodexPluginFile(t, filepath.Join(probeRoot, "README.md")))
	for _, required := range []string{"health-events.jsonl", "host_version", "不记录 prompt"} {
		if !strings.Contains(guide, required) {
			t.Errorf("probe guide omits %q", required)
		}
	}
}

func readCodexPluginFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return contents
}
