package claudecode

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LexLuc/walkout/internal/adapters"
	"github.com/LexLuc/walkout/internal/protocol"
)

var _ adapters.Renderer = (*Renderer)(nil)

func makeEvent(eventType protocol.EventType, capabilities ...protocol.Capability) protocol.HealthEvent {
	return protocol.HealthEvent{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "event-1",
		Provider:      protocol.ProviderClaudeCode,
		HostVersion:   "2.1.224",
		SessionID:     "session-1",
		EventType:     eventType,
		ActivityKind:  protocol.ActivityKindHumanInput,
		Capabilities:  capabilities,
	}
}

func makeDecision(action protocol.DecisionAction, reason protocol.ReasonCode) protocol.HealthDecision {
	state := protocol.StateWalkout
	if action == protocol.ActionInjectReminder {
		state = protocol.StateOvertime
	}
	return protocol.HealthDecision{
		SchemaVersion: protocol.SchemaVersion,
		DecisionID:    "decision-1",
		SourceEventID: "event-1",
		State:         state,
		Action:        action,
		ReasonCode:    reason,
	}
}

func TestRendererBlocksPromptOnlyWithVerifiedCapability(t *testing.T) {
	renderer := NewRenderer()

	blockEvent := makeEvent(protocol.EventTypePromptSubmit, protocol.CapabilityBlockPrompt)
	result, err := renderer.Render(blockEvent, makeDecision(protocol.ActionPausePrompt, protocol.ReasonDebtLimit))
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	if result.ExitCode != 0 || len(result.Stderr) != 0 || result.FailOpen {
		t.Fatalf("result = %#v, want exit 0, empty stderr, no fail-open", result)
	}
	var blockOutput struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(result.Stdout, &blockOutput); err != nil {
		t.Fatalf("stdout is not valid JSON: %v", err)
	}
	if blockOutput.Decision != "block" || blockOutput.Reason == "" {
		t.Fatalf("block output = %#v", blockOutput)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(result.Stdout, &raw); err != nil {
		t.Fatalf("stdout re-parse error = %v", err)
	}
	if len(raw) != 2 {
		t.Fatalf("stdout keys = %d, want exactly decision and reason", len(raw))
	}

	degraded := []struct {
		name     string
		event    protocol.HealthEvent
		decision protocol.HealthDecision
	}{
		{"no capability", makeEvent(protocol.EventTypePromptSubmit), makeDecision(protocol.ActionPausePrompt, protocol.ReasonDebtLimit)},
		{"non-prompt event", makeEvent(protocol.EventTypeSessionStart, protocol.CapabilityBlockPrompt), makeDecision(protocol.ActionPausePrompt, protocol.ReasonDebtLimit)},
		{"allow", makeEvent(protocol.EventTypePromptSubmit, protocol.CapabilityBlockPrompt), makeDecision(protocol.ActionAllow, protocol.ReasonNotDue)},
	}
	for _, test := range degraded {
		t.Run(test.name, func(t *testing.T) {
			result, err := renderer.Render(test.event, test.decision)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if result.ExitCode != 0 || len(result.Stdout) != 0 || len(result.Stderr) != 0 || result.FailOpen {
				t.Fatalf("result = %#v, want silent allow-through", result)
			}
		})
	}
}

func TestRendererWalkoutCopyIsOneBehaviorFocusedSentence(t *testing.T) {
	renderer := NewRenderer()
	for _, reason := range []protocol.ReasonCode{
		protocol.ReasonDebtLimit,
		protocol.ReasonCode("future_reason"),
	} {
		event := makeEvent(protocol.EventTypePromptSubmit, protocol.CapabilityBlockPrompt)
		result, err := renderer.Render(event, makeDecision(protocol.ActionPausePrompt, reason))
		if err != nil {
			t.Fatalf("Render(%s) error = %v", reason, err)
		}
		var blockOutput struct {
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(result.Stdout, &blockOutput); err != nil {
			t.Fatalf("Render(%s) stdout parse error = %v", reason, err)
		}
		copyText := blockOutput.Reason
		if copyText == "" {
			t.Fatalf("Render(%s) produced empty reason copy", reason)
		}
		if strings.Count(copyText, ".") != 1 || !strings.HasSuffix(copyText, ".") {
			t.Errorf("Render(%s) reason %q is not exactly one sentence", reason, copyText)
		}
		for _, required := range []string{"walked out", "/walkout:done", "/walkout:continue"} {
			if !strings.Contains(copyText, required) {
				t.Errorf("Render(%s) reason %q omits %q", reason, copyText, required)
			}
		}
	}
}

func TestRendererInjectsEscalatingReminderOnlyWithInjectCapability(t *testing.T) {
	renderer := NewRenderer()
	renderer.CtlCommand = `C:\plugin\bin\walkout-ctl.exe`

	seen := map[string]bool{}
	for _, reason := range []protocol.ReasonCode{protocol.ReasonBreakDue, protocol.ReasonDebtGrowing} {
		event := makeEvent(protocol.EventTypePromptSubmit, protocol.CapabilityBlockPrompt, protocol.CapabilityInjectContext)
		result, err := renderer.Render(event, makeDecision(protocol.ActionInjectReminder, reason))
		if err != nil {
			t.Fatalf("Render(%s) error = %v", reason, err)
		}
		if result.ExitCode != 0 || len(result.Stderr) != 0 || result.FailOpen {
			t.Fatalf("Render(%s) result = %#v, want exit 0 stdout-only", reason, result)
		}
		var output struct {
			HookSpecificOutput struct {
				HookEventName     string `json:"hookEventName"`
				AdditionalContext string `json:"additionalContext"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal(result.Stdout, &output); err != nil {
			t.Fatalf("Render(%s) stdout is not valid JSON: %v", reason, err)
		}
		if output.HookSpecificOutput.HookEventName != "UserPromptSubmit" {
			t.Fatalf("Render(%s) hookEventName = %q, want UserPromptSubmit", reason, output.HookSpecificOutput.HookEventName)
		}
		copyText := output.HookSpecificOutput.AdditionalContext
		// The instruction names the in-host confirm entry, wires the
		// natural-language confirmation to the bundled ctl binary, asks the
		// model to phrase the reminder itself, and bans system terminology
		// from the user-facing sentence.
		for _, required := range []string{"/walkout:done", renderer.CtlCommand + " done", "in your own words", "system terminology"} {
			if !strings.Contains(copyText, required) {
				t.Errorf("Render(%s) reminder %q omits %q", reason, copyText, required)
			}
		}
		// The situation is stated in plain words so the model has no jargon to
		// parrot; only the ban clause may mention the internal terms.
		facts := strings.SplitN(copyText, ". ", 2)[0]
		for _, jargon := range []string{"debt", "pause limit", "break interval", "walkout"} {
			if strings.Contains(strings.ToLower(facts), jargon) {
				t.Errorf("Render(%s) facts %q leak internal term %q", reason, facts, jargon)
			}
		}
		seen[copyText] = true
	}
	if len(seen) != 2 {
		t.Fatalf("reminder variants = %d, want distinct base and escalated instructions", len(seen))
	}

	degraded := []struct {
		name     string
		event    protocol.HealthEvent
		decision protocol.HealthDecision
	}{
		{"no inject capability", makeEvent(protocol.EventTypePromptSubmit, protocol.CapabilityBlockPrompt), makeDecision(protocol.ActionInjectReminder, protocol.ReasonBreakDue)},
		{"non-prompt event", makeEvent(protocol.EventTypeSessionStart, protocol.CapabilityInjectContext), makeDecision(protocol.ActionInjectReminder, protocol.ReasonBreakDue)},
		{"unmapped reminder reason", makeEvent(protocol.EventTypePromptSubmit, protocol.CapabilityInjectContext), makeDecision(protocol.ActionInjectReminder, protocol.ReasonCode("future_reason"))},
	}
	for _, test := range degraded {
		t.Run(test.name, func(t *testing.T) {
			result, err := renderer.Render(test.event, test.decision)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			if result.ExitCode != 0 || len(result.Stdout) != 0 || len(result.Stderr) != 0 || result.FailOpen {
				t.Fatalf("result = %#v, want silent allow-through", result)
			}
		})
	}
}

func TestRendererRejectsUnknownAction(t *testing.T) {
	renderer := NewRenderer()
	event := makeEvent(protocol.EventTypePromptSubmit, protocol.CapabilityBlockPrompt)
	if _, err := renderer.Render(event, makeDecision(protocol.DecisionAction("future_action"), protocol.ReasonBreakDue)); err == nil {
		t.Fatal("Render() accepted an unknown action, want error for runner fail-open")
	}
}
