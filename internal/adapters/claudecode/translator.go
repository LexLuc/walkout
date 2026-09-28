package claudecode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/protocol"
)

var (
	ErrInvalidHookInput = errors.New("invalid Claude Code hook input")
	ErrUnsupportedHook  = errors.New("unsupported Claude Code hook")
)

// unknownHostVersion is recorded when the host version cannot be determined. It
// is telemetry only; nothing branches on it, so a bundled hook that cannot
// supply the version still functions instead of failing the whole translator.
const unknownHostVersion = "unknown"

type Config struct {
	HostVersion  string
	Capabilities []protocol.Capability
}

type IDGenerator interface {
	NewID() string
}

type Translator struct {
	config Config
	clock  core.Clock
	ids    IDGenerator
}

// hookInput mirrors the Claude Code 2.1.224 hook payload. Unlike Codex, the
// host carries no turn_id or model; prompt_id is the turn-scoped identifier
// and also appears on SessionEnd. Work content fields exist only inside this
// hook process and must never reach the HealthEvent.
type hookInput struct {
	SessionID        string          `json:"session_id"`
	PromptID         *string         `json:"prompt_id"`
	HookEventName    string          `json:"hook_event_name"`
	Source           *string         `json:"source"`
	Prompt           *string         `json:"prompt"`
	StopHookActive   *bool           `json:"stop_hook_active"`
	Reason           *string         `json:"reason"`
	PermissionMode   *string         `json:"permission_mode"`
	TranscriptPath   *string         `json:"transcript_path"`
	CWD              *string         `json:"cwd"`
	AssistantMessage *string         `json:"last_assistant_message"`
	BackgroundTasks  json.RawMessage `json:"background_tasks"`
	SessionCrons     json.RawMessage `json:"session_crons"`
}

func NewTranslator(config Config, clock core.Clock, ids IDGenerator) (*Translator, error) {
	if config.HostVersion == "" {
		config.HostVersion = unknownHostVersion
	}
	if clock == nil {
		return nil, errors.New("clock is required")
	}
	if ids == nil {
		return nil, errors.New("ID generator is required")
	}
	config.Capabilities = append([]protocol.Capability(nil), config.Capabilities...)
	return &Translator{config: config, clock: clock, ids: ids}, nil
}

func (t *Translator) Translate(encoded []byte) (protocol.HealthEvent, error) {
	input, err := decodeHookInput(encoded)
	if err != nil {
		return protocol.HealthEvent{}, err
	}
	if input.SessionID == "" || input.HookEventName == "" {
		return protocol.HealthEvent{}, fmt.Errorf("%w: required envelope field missing", ErrInvalidHookInput)
	}
	eventType, activity, turnID, err := classify(input)
	if err != nil {
		return protocol.HealthEvent{}, err
	}
	eventID := t.ids.NewID()
	if eventID == "" {
		return protocol.HealthEvent{}, errors.New("ID generator returned an empty event ID")
	}
	return protocol.HealthEvent{
		SchemaVersion: protocol.SchemaVersion,
		EventID:       eventID,
		Provider:      protocol.ProviderClaudeCode,
		HostVersion:   t.config.HostVersion,
		SessionID:     input.SessionID,
		TurnID:        turnID,
		ToolCallID:    nil,
		EventType:     eventType,
		OccurredAt:    t.clock.Now(),
		ActivityKind:  activity,
		Capabilities:  append([]protocol.Capability(nil), t.config.Capabilities...),
		Payload:       map[string]json.RawMessage{},
	}, nil
}

func decodeHookInput(encoded []byte) (hookInput, error) {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	var input hookInput
	if err := decoder.Decode(&input); err != nil {
		return hookInput{}, fmt.Errorf("%w: malformed JSON", ErrInvalidHookInput)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return hookInput{}, fmt.Errorf("%w: multiple JSON values", ErrInvalidHookInput)
	}
	return input, nil
}

func classify(input hookInput) (protocol.EventType, protocol.ActivityKind, *string, error) {
	switch input.HookEventName {
	case "SessionStart":
		if input.Source == nil || !validSessionSource(*input.Source) || input.PromptID != nil {
			return "", "", nil, fmt.Errorf("%w: invalid SessionStart fields", ErrInvalidHookInput)
		}
		return protocol.EventTypeSessionStart, protocol.ActivityKindSessionLifecycle, nil, nil
	case "UserPromptSubmit":
		if emptyString(input.PromptID) || input.Prompt == nil {
			return "", "", nil, fmt.Errorf("%w: invalid UserPromptSubmit fields", ErrInvalidHookInput)
		}
		return protocol.EventTypePromptSubmit, protocol.ActivityKindHumanInput, cloneString(input.PromptID), nil
	case "Stop":
		if emptyString(input.PromptID) || input.StopHookActive == nil {
			return "", "", nil, fmt.Errorf("%w: invalid Stop fields", ErrInvalidHookInput)
		}
		return protocol.EventTypeStop, protocol.ActivityKindAgentWork, cloneString(input.PromptID), nil
	case "SessionEnd":
		// prompt_id was recorded on SessionEnd for prompt-carrying sessions,
		// but a session can end before any prompt exists, so it stays optional.
		if input.Reason == nil || *input.Reason == "" || (input.PromptID != nil && *input.PromptID == "") {
			return "", "", nil, fmt.Errorf("%w: invalid SessionEnd fields", ErrInvalidHookInput)
		}
		return protocol.EventTypeSessionEnd, protocol.ActivityKindSessionLifecycle, cloneString(input.PromptID), nil
	default:
		return "", "", nil, fmt.Errorf("%w: %s", ErrUnsupportedHook, input.HookEventName)
	}
}

func validSessionSource(source string) bool {
	switch source {
	case "startup", "resume", "clear", "compact":
		return true
	default:
		return false
	}
}

func emptyString(value *string) bool {
	return value == nil || *value == ""
}

func cloneString(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
