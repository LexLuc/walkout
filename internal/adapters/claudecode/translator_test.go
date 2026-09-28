package claudecode

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LexLuc/walkout/internal/protocol"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type sequenceIDs struct {
	next int
}

func (g *sequenceIDs) NewID() string {
	g.next++
	return fmt.Sprintf("event-%d", g.next)
}

func TestTranslatorMapsRecordedClaudeCodeFixturesWithoutRetainingWorkContent(t *testing.T) {
	now := time.Date(2026, 8, 8, 5, 0, 0, 0, time.UTC)
	translator, err := NewTranslator(Config{
		HostVersion:  "2.1.224",
		Capabilities: []protocol.Capability{protocol.CapabilityBlockPrompt},
	}, fixedClock{now: now}, &sequenceIDs{})
	if err != nil {
		t.Fatalf("NewTranslator() error = %v", err)
	}

	tests := []struct {
		fixture     string
		eventType   protocol.EventType
		activity    protocol.ActivityKind
		wantTurnID  bool
		wantEventID string
	}{
		{"session_start.json", protocol.EventTypeSessionStart, protocol.ActivityKindSessionLifecycle, false, "event-1"},
		{"user_prompt_submit.json", protocol.EventTypePromptSubmit, protocol.ActivityKindHumanInput, true, "event-2"},
		{"stop.json", protocol.EventTypeStop, protocol.ActivityKindAgentWork, true, "event-3"},
		{"session_end.json", protocol.EventTypeSessionEnd, protocol.ActivityKindSessionLifecycle, true, "event-4"},
	}
	for _, test := range tests {
		t.Run(test.fixture, func(t *testing.T) {
			encoded := readFixture(t, test.fixture)
			event, err := translator.Translate(encoded)
			if err != nil {
				t.Fatalf("Translate() error = %v", err)
			}
			if event.SchemaVersion != protocol.SchemaVersion || event.Provider != protocol.ProviderClaudeCode {
				t.Fatalf("protocol identity = %q/%q", event.SchemaVersion, event.Provider)
			}
			if event.HostVersion != "2.1.224" || event.SessionID != "session-fixture" {
				t.Fatalf("host/session = %q/%q", event.HostVersion, event.SessionID)
			}
			if event.EventID != test.wantEventID || event.EventType != test.eventType || event.ActivityKind != test.activity {
				t.Fatalf("event = %#v", event)
			}
			if (event.TurnID != nil) != test.wantTurnID {
				t.Fatalf("TurnID = %v, want present %v", event.TurnID, test.wantTurnID)
			}
			if test.wantTurnID && *event.TurnID != "prompt-fixture" {
				t.Fatalf("TurnID = %q, want prompt-fixture", *event.TurnID)
			}
			if event.ToolCallID != nil || !event.OccurredAt.Equal(now) {
				t.Fatalf("tool/time = %v/%v", event.ToolCallID, event.OccurredAt)
			}
			if len(event.Capabilities) != 1 || event.Capabilities[0] != protocol.CapabilityBlockPrompt {
				t.Fatalf("capabilities = %v", event.Capabilities)
			}
			if len(event.Payload) != 0 {
				t.Fatalf("payload = %v, want empty", event.Payload)
			}

			healthJSON, err := json.Marshal(event)
			if err != nil {
				t.Fatalf("json.Marshal(event) error = %v", err)
			}
			for _, forbidden := range []string{
				"CONFIDENTIAL_KNOWLEDGE_WORK_PROMPT",
				"CONFIDENTIAL_ASSISTANT_MESSAGE",
				"<redacted-cwd>",
				"<redacted-transcript-path>",
				"transcript_path",
				"permission_mode",
			} {
				if strings.Contains(string(healthJSON), forbidden) {
					t.Errorf("HealthEvent leaked %q", forbidden)
				}
			}
		})
	}
}

func TestTranslatorFailsSafelyForInvalidOrUnsupportedInput(t *testing.T) {
	translator, err := NewTranslator(
		Config{HostVersion: "2.1.224"},
		fixedClock{now: time.Now()},
		&sequenceIDs{},
	)
	if err != nil {
		t.Fatalf("NewTranslator() error = %v", err)
	}
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"malformed", `{`, ErrInvalidHookInput},
		{"missing session", `{"hook_event_name":"SessionStart","source":"startup"}`, ErrInvalidHookInput},
		{"invalid session source", `{"session_id":"s","hook_event_name":"SessionStart","source":"unexpected"}`, ErrInvalidHookInput},
		{"missing prompt id on prompt", `{"session_id":"s","hook_event_name":"UserPromptSubmit","prompt":"x"}`, ErrInvalidHookInput},
		{"missing prompt id on stop", `{"session_id":"s","hook_event_name":"Stop","stop_hook_active":false}`, ErrInvalidHookInput},
		{"missing reason on session end", `{"session_id":"s","hook_event_name":"SessionEnd"}`, ErrInvalidHookInput},
		{"unsupported event", `{"session_id":"s","hook_event_name":"FutureEvent"}`, ErrUnsupportedHook},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := translator.Translate([]byte(test.input))
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Translate() error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestTranslatorToleratesAdditionalHostFieldsAndOptionalSessionEndPromptID(t *testing.T) {
	translator, err := NewTranslator(
		Config{HostVersion: "2.1.224"},
		fixedClock{now: time.Now()},
		&sequenceIDs{},
	)
	if err != nil {
		t.Fatalf("NewTranslator() error = %v", err)
	}
	additive := `{"session_id":"s","hook_event_name":"SessionStart","source":"startup","future_field":{"nested":true}}`
	if _, err := translator.Translate([]byte(additive)); err != nil {
		t.Fatalf("Translate() rejected additive host field: %v", err)
	}
	// A session can end before any prompt was submitted; prompt_id presence on
	// SessionEnd is only proven for prompt-carrying sessions.
	promptless := `{"session_id":"s","hook_event_name":"SessionEnd","reason":"other"}`
	event, err := translator.Translate([]byte(promptless))
	if err != nil {
		t.Fatalf("Translate() rejected SessionEnd without prompt_id: %v", err)
	}
	if event.TurnID != nil {
		t.Fatalf("TurnID = %v, want nil for promptless SessionEnd", event.TurnID)
	}
}

func TestTranslatorDefaultsMissingHostVersionToUnknown(t *testing.T) {
	// A bundled hook may not always supply the host version. Rejecting it here
	// would fail NewTranslator and silently disable the guard via fail-open, so
	// the version (telemetry only) must default to a sentinel instead.
	translator, err := NewTranslator(
		Config{Capabilities: []protocol.Capability{protocol.CapabilityBlockPrompt}},
		fixedClock{now: time.Now()},
		&sequenceIDs{},
	)
	if err != nil {
		t.Fatalf("NewTranslator() with empty host version error = %v", err)
	}
	event, err := translator.Translate([]byte(`{"session_id":"s","hook_event_name":"SessionStart","source":"startup"}`))
	if err != nil {
		t.Fatalf("Translate() error = %v", err)
	}
	if event.HostVersion != "unknown" {
		t.Fatalf("HostVersion = %q, want %q", event.HostVersion, "unknown")
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	encoded, err := os.ReadFile(filepath.Join("testdata", "v2.1.224", name))
	if err != nil {
		t.Fatalf("read fixture error = %v", err)
	}
	return encoded
}
