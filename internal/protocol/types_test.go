package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestHealthEventJSONContract(t *testing.T) {
	t.Parallel()

	turnID := "turn-7"
	event := HealthEvent{
		SchemaVersion: SchemaVersion,
		EventID:       "018f4a58-6a8c-7c68-a0d8-04f5c1e5df8f",
		Provider:      ProviderCodex,
		HostVersion:   "1.2.3",
		SessionID:     "session-a",
		TurnID:        &turnID,
		EventType:     EventTypePromptSubmit,
		OccurredAt:    time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC),
		ActivityKind:  ActivityKindHumanInput,
		Capabilities:  []Capability{CapabilityBlockPrompt, CapabilityInjectContext},
		Payload:       map[string]json.RawMessage{},
	}

	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	wantKeys := []string{
		"schema_version", "event_id", "provider", "host_version", "session_id",
		"turn_id", "tool_call_id", "event_type", "occurred_at", "activity_kind",
		"capabilities", "payload",
	}
	assertExactKeys(t, got, wantKeys)

	if got["schema_version"] != "1.0" {
		t.Fatalf("schema_version = %v, want 1.0", got["schema_version"])
	}
	if got["provider"] != "codex" {
		t.Fatalf("provider = %v, want codex", got["provider"])
	}
	if got["event_type"] != "prompt_submit" {
		t.Fatalf("event_type = %v, want prompt_submit", got["event_type"])
	}
	if got["tool_call_id"] != nil {
		t.Fatalf("tool_call_id = %v, want null", got["tool_call_id"])
	}
}

func TestHealthDecisionJSONContainsNoFreeText(t *testing.T) {
	t.Parallel()

	decision := HealthDecision{
		SchemaVersion:  SchemaVersion,
		DecisionID:     "302cf2ef-dc38-5e9d-a37b-b1a5a73d4a01",
		SourceEventID:  "018f4a58-6a8c-7c68-a0d8-04f5c1e5df8f",
		StateRevision:  42,
		State:          StateOvertime,
		Action:         ActionInjectReminder,
		ReasonCode:     ReasonBreakDue,
		InterventionID: stringPointer("intervention-1"),
	}

	encoded, err := json.Marshal(decision)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	wantKeys := []string{
		"schema_version", "decision_id", "source_event_id", "state_revision", "state",
		"action", "reason_code", "intervention_id", "protected_until", "next_check_at",
	}
	assertExactKeys(t, got, wantKeys)
	for _, forbidden := range []string{"message", "reason", "reminder_seed", "safe_breakpoint_label"} {
		if _, exists := got[forbidden]; exists {
			t.Fatalf("HealthDecision contains forbidden free-text field %q", forbidden)
		}
	}
}

func TestHealthControlProtocolIsHostNeutralAndStructured(t *testing.T) {
	t.Parallel()

	command := HealthControlCommand{
		SchemaVersion: SchemaVersion,
		CommandID:     "command-1",
		CommandType:   CommandConfirmActivity,
	}
	encoded, err := json.Marshal(command)
	if err != nil {
		t.Fatalf("json.Marshal(command) error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("json.Unmarshal(command) error = %v", err)
	}
	assertExactKeys(t, got, []string{"schema_version", "command_id", "command_type"})
	for _, forbidden := range []string{"provider", "session_id", "message", "work_context"} {
		if _, exists := got[forbidden]; exists {
			t.Fatalf("HealthControlCommand contains host or free-text field %q", forbidden)
		}
	}

	response := HealthControlResponse{
		SchemaVersion: SchemaVersion,
		CommandID:     command.CommandID,
		CommandType:   command.CommandType,
		Status: HealthStatus{
			SchemaVersion:         SchemaVersion,
			State:                 StateWorking,
			StateRevision:         1,
			ContinuousWorkSeconds: 120,
			HealthDebtSeconds:     0,
			EngagementActive:      false,
			ActivityCount:         0,
		},
	}
	encoded, err = json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal(response) error = %v", err)
	}
	got = map[string]any{}
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("json.Unmarshal(response) error = %v", err)
	}
	assertExactKeys(t, got, []string{"schema_version", "command_id", "command_type", "status"})
	status, ok := got["status"].(map[string]any)
	if !ok {
		t.Fatalf("status JSON = %T, want object", got["status"])
	}
	assertExactKeys(t, status, []string{
		"schema_version", "state", "state_revision", "continuous_work_seconds",
		"health_debt_seconds", "engagement_active", "activity_count",
		"emergency_continue_active", "emergency_continue_remaining_seconds",
		"emergency_continue_reason",
	})
}

func TestHealthControlCommandCarriesEmergencyReason(t *testing.T) {
	t.Parallel()

	withReason, err := json.Marshal(HealthControlCommand{
		SchemaVersion: SchemaVersion,
		CommandID:     "command-2",
		CommandType:   CommandEmergencyContinue,
		Reason:        "prod incident",
	})
	if err != nil {
		t.Fatalf("json.Marshal(withReason) error = %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(withReason, &got); err != nil {
		t.Fatalf("json.Unmarshal(withReason) error = %v", err)
	}
	if got["reason"] != "prod incident" {
		t.Fatalf("reason = %v, want \"prod incident\"", got["reason"])
	}

	// A command without a reason must not emit the key, keeping the
	// confirm_activity payload byte-for-byte stable.
	bare, err := json.Marshal(HealthControlCommand{
		SchemaVersion: SchemaVersion,
		CommandID:     "command-3",
		CommandType:   CommandConfirmActivity,
	})
	if err != nil {
		t.Fatalf("json.Marshal(bare) error = %v", err)
	}
	var bareMap map[string]any
	if err := json.Unmarshal(bare, &bareMap); err != nil {
		t.Fatalf("json.Unmarshal(bare) error = %v", err)
	}
	if _, exists := bareMap["reason"]; exists {
		t.Fatal("reason key present on a command with no reason, want omitempty")
	}
}

func assertExactKeys(t *testing.T, got map[string]any, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("JSON key count = %d, want %d: %v", len(got), len(want), got)
	}
	for _, key := range want {
		if _, exists := got[key]; !exists {
			t.Errorf("JSON missing key %q", key)
		}
	}
}

func stringPointer(value string) *string {
	return &value
}
