package codex

import (
	"fmt"
	"strings"

	"github.com/LexLuc/walkout/internal/protocol"
)

// controlMarkers maps each explicit Codex conversation marker to its
// host-neutral control command. Only continue accepts a trailing reason; the
// recovery-loop markers must be the entire prompt so ordinary work text can
// never be misread as a control request.
var controlMarkers = map[string]struct {
	command      protocol.HealthControlCommandType
	acceptReason bool
}{
	"$walkout:continue": {command: protocol.CommandEmergencyContinue, acceptReason: true},
	"$walkout:done":     {command: protocol.CommandConfirmActivity},
}

// ControlRequest carries only the resolved control command and its optional
// reason. It deliberately cannot carry the original prompt or any session
// work content.
type ControlRequest struct {
	Command protocol.HealthControlCommandType
	Reason  string
}

// ParseControlRequest recognizes an explicit Codex control marker at the very
// start of a structurally valid UserPromptSubmit payload. A non-match returns
// a zero request so ordinary prompt content cannot leave this adapter.
func ParseControlRequest(encoded []byte) (ControlRequest, bool, error) {
	input, err := decodeHookInput(encoded)
	if err != nil {
		return ControlRequest{}, false, err
	}
	if input.SessionID == "" || input.HookEventName == "" {
		return ControlRequest{}, false, fmt.Errorf("%w: required envelope field missing", ErrInvalidHookInput)
	}
	eventType, _, err := classify(input)
	if err != nil {
		return ControlRequest{}, false, err
	}
	if eventType != protocol.EventTypePromptSubmit {
		return ControlRequest{}, false, nil
	}

	prompt := *input.Prompt
	for marker, spec := range controlMarkers {
		if prompt == marker {
			return ControlRequest{Command: spec.command}, true, nil
		}
		if !spec.acceptReason || !strings.HasPrefix(prompt, marker) {
			continue
		}
		remainder := prompt[len(marker):]
		if remainder == "" || (remainder[0] != ' ' && remainder[0] != '\t') {
			continue
		}
		if strings.ContainsAny(remainder, "\r\n") {
			continue
		}
		return ControlRequest{Command: spec.command, Reason: strings.Trim(remainder, " \t")}, true, nil
	}
	return ControlRequest{}, false, nil
}
