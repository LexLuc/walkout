package codex

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
	ErrInvalidHookInput = errors.New("invalid Codex hook input")
	ErrUnsupportedHook  = errors.New("unsupported Codex hook")
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

type hookInput struct {
	SessionID        string  `json:"session_id"`
	TurnID           *string `json:"turn_id"`
	HookEventName    string  `json:"hook_event_name"`
	Source           *string `json:"source"`
	Prompt           *string `json:"prompt"`
	StopHookActive   *bool   `json:"stop_hook_active"`
	Reason           *string `json:"reason"`
	PermissionMode   *string `json:"permission_mode"`
	Model            *string `json:"model"`
	TranscriptPath   *string `json:"transcript_path"`
	CWD              *string `json:"cwd"`
	AssistantMessage *string `json:"last_assistant_message"`
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
	eventType, activity, err := classify(input)
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
		Provider:      protocol.ProviderCodex,
		HostVersion:   t.config.HostVersion,
		SessionID:     input.SessionID,
		TurnID:        cloneString(input.TurnID),
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

func classify(input hookInput) (protocol.EventType, protocol.ActivityKind, error) {
	switch input.HookEventName {
	case "SessionStart":
		if input.Source == nil || !validSessionSource(*input.Source) || input.TurnID != nil {
			return "", "", fmt.Errorf("%w: invalid SessionStart fields", ErrInvalidHookInput)
		}
		return protocol.EventTypeSessionStart, protocol.ActivityKindSessionLifecycle, nil
	case "UserPromptSubmit":
		if emptyString(input.TurnID) || input.Prompt == nil {
			return "", "", fmt.Errorf("%w: invalid UserPromptSubmit fields", ErrInvalidHookInput)
		}
		return protocol.EventTypePromptSubmit, protocol.ActivityKindHumanInput, nil
	case "Stop":
		if emptyString(input.TurnID) || input.StopHookActive == nil {
			return "", "", fmt.Errorf("%w: invalid Stop fields", ErrInvalidHookInput)
		}
		return protocol.EventTypeStop, protocol.ActivityKindAgentWork, nil
	case "SessionEnd":
		if input.Reason == nil || *input.Reason == "" || input.TurnID != nil {
			return "", "", fmt.Errorf("%w: invalid SessionEnd fields", ErrInvalidHookInput)
		}
		return protocol.EventTypeSessionEnd, protocol.ActivityKindSessionLifecycle, nil
	default:
		return "", "", fmt.Errorf("%w: %s", ErrUnsupportedHook, input.HookEventName)
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
