package protocol

import (
	"encoding/json"
	"strings"
	"time"
)

const SchemaVersion = "1.0"

func IsSupportedSchema(version string) bool {
	major, _, found := strings.Cut(version, ".")
	return found && major == "1"
}

type Provider string

const (
	ProviderCodex      Provider = "codex"
	ProviderClaudeCode Provider = "claude_code"
	ProviderWorkBuddy  Provider = "workbuddy"
)

type EventType string

const (
	EventTypeSessionStart EventType = "session_start"
	EventTypePromptSubmit EventType = "prompt_submit"
	EventTypePreTool      EventType = "pre_tool"
	EventTypePostTool     EventType = "post_tool"
	EventTypeStop         EventType = "stop"
	EventTypeSessionEnd   EventType = "session_end"
)

type ActivityKind string

const (
	ActivityKindHumanInput       ActivityKind = "human_input"
	ActivityKindAgentWork        ActivityKind = "agent_work"
	ActivityKindToolWork         ActivityKind = "tool_work"
	ActivityKindSessionLifecycle ActivityKind = "session_lifecycle"
)

type Capability string

const (
	CapabilityBlockPrompt       Capability = "block_prompt"
	CapabilityContinueAfterStop Capability = "continue_after_stop"
	CapabilityInjectContext     Capability = "inject_context"
	CapabilityToolHeartbeat     Capability = "tool_heartbeat"
)

// HealthEvent is the host-neutral event sent from an adapter to walkoutd.
type HealthEvent struct {
	SchemaVersion string                     `json:"schema_version"`
	EventID       string                     `json:"event_id"`
	Provider      Provider                   `json:"provider"`
	HostVersion   string                     `json:"host_version"`
	SessionID     string                     `json:"session_id"`
	TurnID        *string                    `json:"turn_id"`
	ToolCallID    *string                    `json:"tool_call_id"`
	EventType     EventType                  `json:"event_type"`
	OccurredAt    time.Time                  `json:"occurred_at"`
	ActivityKind  ActivityKind               `json:"activity_kind"`
	Capabilities  []Capability               `json:"capabilities"`
	Payload       map[string]json.RawMessage `json:"payload"`
}

type State string

// The user-visible state machine is deliberately three states (simplified per
// the 2026-08-26 product ruling): reminder escalation, debt depth, and the
// emergency lease are fields inside these states, not states of their own.
const (
	StateWorking  State = "working"
	StateOvertime State = "overtime"
	StateWalkout  State = "walkout"
)

type DecisionAction string

const (
	ActionAllow          DecisionAction = "allow"
	ActionInjectReminder DecisionAction = "inject_reminder"
	ActionPausePrompt    DecisionAction = "pause_prompt"
	ActionFailOpen       DecisionAction = "fail_open"
)

type ReasonCode string

const (
	ReasonNotDue             ReasonCode = "not_due"
	ReasonBreakDue           ReasonCode = "break_due"
	ReasonDebtGrowing        ReasonCode = "debt_growing"
	ReasonProtectedWindow    ReasonCode = "protected_window"
	ReasonDebtLimit          ReasonCode = "debt_limit"
	ReasonDaemonUnavailable  ReasonCode = "daemon_unavailable"
	ReasonSchemaIncompatible ReasonCode = "schema_incompatible"
	ReasonEmergencyContinue  ReasonCode = "emergency_continue"
)

// HealthDecision is the host-neutral deterministic decision returned by
// walkoutd. It intentionally contains no free-text reminder content.
type HealthDecision struct {
	SchemaVersion  string         `json:"schema_version"`
	DecisionID     string         `json:"decision_id"`
	SourceEventID  string         `json:"source_event_id"`
	StateRevision  uint64         `json:"state_revision"`
	State          State          `json:"state"`
	Action         DecisionAction `json:"action"`
	ReasonCode     ReasonCode     `json:"reason_code"`
	InterventionID *string        `json:"intervention_id"`
	ProtectedUntil *time.Time     `json:"protected_until"`
	NextCheckAt    *time.Time     `json:"next_check_at"`
}

type HealthControlCommandType string

const (
	// CommandConfirmActivity is the single-step "I moved" confirmation: it
	// clears continuous work and health debt and returns to working from any
	// state. There is no separate start-break, confirm-recovery, or snooze.
	CommandConfirmActivity   HealthControlCommandType = "confirm_activity"
	CommandEmergencyContinue HealthControlCommandType = "emergency_continue"
)

// HealthControlCommand is a host-neutral user control request. It deliberately
// excludes session details and free-text work context.
type HealthControlCommand struct {
	SchemaVersion string                   `json:"schema_version"`
	CommandID     string                   `json:"command_id"`
	CommandType   HealthControlCommandType `json:"command_type"`
	// Reason is optional user-supplied control metadata recorded when the user
	// overrides a pause with emergency_continue. It is omitted from other
	// commands so their payloads stay byte-for-byte unchanged.
	Reason string `json:"reason,omitempty"`
}

type HealthStatus struct {
	SchemaVersion                    string `json:"schema_version"`
	State                            State  `json:"state"`
	StateRevision                    uint64 `json:"state_revision"`
	ContinuousWorkSeconds            int64  `json:"continuous_work_seconds"`
	HealthDebtSeconds                int64  `json:"health_debt_seconds"`
	EngagementActive                 bool   `json:"engagement_active"`
	ActivityCount                    uint64 `json:"activity_count"`
	EmergencyContinueActive          bool   `json:"emergency_continue_active"`
	EmergencyContinueRemainingSecond int64  `json:"emergency_continue_remaining_seconds"`
	EmergencyContinueReason          string `json:"emergency_continue_reason"`
}

type HealthControlResponse struct {
	SchemaVersion string                   `json:"schema_version"`
	CommandID     string                   `json:"command_id"`
	CommandType   HealthControlCommandType `json:"command_type"`
	Status        HealthStatus             `json:"status"`
}
