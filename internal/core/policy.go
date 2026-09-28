package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/LexLuc/walkout/internal/protocol"
)

type AssessmentStatus string

const (
	AssessmentMissing           AssessmentStatus = "missing"
	AssessmentSafeNow           AssessmentStatus = "safe_now"
	AssessmentFinishCurrentStep AssessmentStatus = "finish_current_step"
)

type PolicyInput struct {
	Event         protocol.HealthEvent
	State         protocol.State
	StateRevision uint64
	// HealthDebt distinguishes the early overtime reminder (no debt yet) from
	// the escalated one (debt accruing); it never changes the action itself.
	HealthDebt time.Duration
	// AssessmentStatus is an optional enhancer since the 2026-08-26 ruling: it
	// may refine reminder wording downstream but never gates whether the
	// reminder fires.
	AssessmentStatus     AssessmentStatus
	InterventionID       string
	EmergencyLeaseActive bool
}

// Policy evaluates host-neutral inputs without I/O or natural-language
// interpretation. The same input always produces the same decision.
type Policy struct{}

func NewPolicy() Policy {
	return Policy{}
}

func (Policy) Decide(input PolicyInput) protocol.HealthDecision {
	action, reason := decideAction(input)

	decision := protocol.HealthDecision{
		SchemaVersion: protocol.SchemaVersion,
		SourceEventID: input.Event.EventID,
		StateRevision: input.StateRevision,
		State:         input.State,
		Action:        action,
		ReasonCode:    reason,
	}
	if input.InterventionID != "" {
		interventionID := input.InterventionID
		decision.InterventionID = &interventionID
	}
	decision.DecisionID = deterministicDecisionID(decision)
	return decision
}

func decideAction(input PolicyInput) (protocol.DecisionAction, protocol.ReasonCode) {
	if !protocol.IsSupportedSchema(input.Event.SchemaVersion) {
		return protocol.ActionFailOpen, protocol.ReasonSchemaIncompatible
	}
	if input.State == protocol.StateWalkout && input.Event.EventType == protocol.EventTypePromptSubmit {
		// A live emergency-continue lease is the opted-in escape hatch: it lets
		// this prompt through without forgiving accrued debt. The lease active
		// flag is computed against the clock outside the pure policy.
		if input.EmergencyLeaseActive {
			return protocol.ActionAllow, protocol.ReasonEmergencyContinue
		}
		return protocol.ActionPausePrompt, protocol.ReasonDebtLimit
	}
	if input.State == protocol.StateOvertime && input.Event.EventType == protocol.EventTypePromptSubmit {
		// Overtime never blocks: the prompt is admitted and every submission
		// carries a reminder. The reminder does not depend on an assessment.
		if input.HealthDebt > 0 {
			return protocol.ActionInjectReminder, protocol.ReasonDebtGrowing
		}
		return protocol.ActionInjectReminder, protocol.ReasonBreakDue
	}
	return protocol.ActionAllow, protocol.ReasonNotDue
}

func deterministicDecisionID(decision protocol.HealthDecision) string {
	material := fmt.Sprintf(
		"%s|%s|%d|%s|%s|%s|%s",
		decision.SchemaVersion,
		decision.SourceEventID,
		decision.StateRevision,
		decision.State,
		decision.Action,
		decision.ReasonCode,
		optionalString(decision.InterventionID),
	)
	sum := sha256.Sum256([]byte(material))
	bytes := sum[:16]
	bytes[6] = (bytes[6] & 0x0f) | 0x50
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	encoded := hex.EncodeToString(bytes)
	return fmt.Sprintf("%s-%s-%s-%s-%s", encoded[0:8], encoded[8:12], encoded[12:16], encoded[16:20], encoded[20:32])
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
