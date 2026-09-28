package core

import (
	"reflect"
	"testing"
	"time"

	"github.com/LexLuc/walkout/internal/protocol"
)

func TestPolicyDecisions(t *testing.T) {
	t.Parallel()

	baseEvent := protocol.HealthEvent{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       "018f4a58-6a8c-7c68-a0d8-04f5c1e5df8f",
		Provider:      protocol.ProviderCodex,
		HostVersion:   "1.2.3",
		SessionID:     "session-a",
		EventType:     protocol.EventTypePromptSubmit,
		OccurredAt:    time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC),
		ActivityKind:  protocol.ActivityKindHumanInput,
	}

	tests := []struct {
		name       string
		input      PolicyInput
		wantAction protocol.DecisionAction
		wantReason protocol.ReasonCode
	}{
		{
			name: "normal work is allowed",
			input: PolicyInput{
				Event:         baseEvent,
				State:         protocol.StateWorking,
				StateRevision: 1,
			},
			wantAction: protocol.ActionAllow,
			wantReason: protocol.ReasonNotDue,
		},
		{
			name: "an incompatible schema fails open",
			input: PolicyInput{
				Event:         withSchema(baseEvent, "2.0"),
				State:         protocol.StateWalkout,
				StateRevision: 2,
			},
			wantAction: protocol.ActionFailOpen,
			wantReason: protocol.ReasonSchemaIncompatible,
		},
		{
			name: "a paused service pauses the next prompt",
			input: PolicyInput{
				Event:         baseEvent,
				State:         protocol.StateWalkout,
				StateRevision: 3,
			},
			wantAction: protocol.ActionPausePrompt,
			wantReason: protocol.ReasonDebtLimit,
		},
		{
			name: "an active emergency lease allows a prompt while paused",
			input: PolicyInput{
				Event:                baseEvent,
				State:                protocol.StateWalkout,
				StateRevision:        3,
				EmergencyLeaseActive: true,
			},
			wantAction: protocol.ActionAllow,
			wantReason: protocol.ReasonEmergencyContinue,
		},
		{
			name: "an expired emergency lease still pauses the prompt",
			input: PolicyInput{
				Event:                baseEvent,
				State:                protocol.StateWalkout,
				StateRevision:        3,
				EmergencyLeaseActive: false,
			},
			wantAction: protocol.ActionPausePrompt,
			wantReason: protocol.ReasonDebtLimit,
		},
		{
			name: "overtime injects a reminder even without an assessment",
			input: PolicyInput{
				Event:            baseEvent,
				State:            protocol.StateOvertime,
				StateRevision:    4,
				AssessmentStatus: AssessmentMissing,
			},
			wantAction: protocol.ActionInjectReminder,
			wantReason: protocol.ReasonBreakDue,
		},
		{
			name: "overtime with accrued debt escalates the reminder reason",
			input: PolicyInput{
				Event:            baseEvent,
				State:            protocol.StateOvertime,
				StateRevision:    5,
				HealthDebt:       3 * time.Minute,
				AssessmentStatus: AssessmentSafeNow,
				InterventionID:   "intervention-5",
			},
			wantAction: protocol.ActionInjectReminder,
			wantReason: protocol.ReasonDebtGrowing,
		},
		{
			name: "overtime never blocks non-prompt events",
			input: PolicyInput{
				Event: func() protocol.HealthEvent {
					event := baseEvent
					event.EventType = protocol.EventTypeStop
					event.ActivityKind = protocol.ActivityKindAgentWork
					return event
				}(),
				State:         protocol.StateOvertime,
				StateRevision: 6,
			},
			wantAction: protocol.ActionAllow,
			wantReason: protocol.ReasonNotDue,
		},
	}

	policy := NewPolicy()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			decision := policy.Decide(test.input)
			if decision.Action != test.wantAction {
				t.Fatalf("Action = %q, want %q", decision.Action, test.wantAction)
			}
			if decision.ReasonCode != test.wantReason {
				t.Fatalf("ReasonCode = %q, want %q", decision.ReasonCode, test.wantReason)
			}
			if decision.SourceEventID != test.input.Event.EventID {
				t.Fatalf("SourceEventID = %q, want %q", decision.SourceEventID, test.input.Event.EventID)
			}
			if decision.StateRevision != test.input.StateRevision {
				t.Fatalf("StateRevision = %d, want %d", decision.StateRevision, test.input.StateRevision)
			}
			if decision.DecisionID == "" {
				t.Fatal("DecisionID is empty")
			}
		})
	}
}

func TestPolicyIsDeterministicForTheSameInput(t *testing.T) {
	t.Parallel()

	input := PolicyInput{
		Event: protocol.HealthEvent{
			SchemaVersion: protocol.SchemaVersion,
			EventID:       "018f4a58-6a8c-7c68-a0d8-04f5c1e5df8f",
			Provider:      protocol.ProviderClaudeCode,
			SessionID:     "session-b",
			EventType:     protocol.EventTypeStop,
			OccurredAt:    time.Date(2026, 8, 6, 10, 0, 0, 0, time.UTC),
			ActivityKind:  protocol.ActivityKindAgentWork,
		},
		State:            protocol.StateOvertime,
		StateRevision:    8,
		AssessmentStatus: AssessmentSafeNow,
		InterventionID:   "intervention-8",
	}

	policy := NewPolicy()
	first := policy.Decide(input)
	second := policy.Decide(input)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same input produced different decisions:\nfirst:  %#v\nsecond: %#v", first, second)
	}
}

func withSchema(event protocol.HealthEvent, schemaVersion string) protocol.HealthEvent {
	event.SchemaVersion = schemaVersion
	return event
}
