package codex

import (
	"strings"
	"testing"

	"github.com/LexLuc/walkout/internal/protocol"
)

func TestParseControlRequestUsesAnExactPromptPrefix(t *testing.T) {
	t.Parallel()

	recorded := string(readFixtureVersion(t, "v0.146.0", "user_prompt_submit_emergency_continue.json"))
	tests := []struct {
		name        string
		input       string
		wantMatch   bool
		wantCommand protocol.HealthControlCommandType
		wantReason  string
		wantErr     bool
	}{
		{
			name:        "recorded skill mention with reason",
			input:       recorded,
			wantMatch:   true,
			wantCommand: protocol.CommandEmergencyContinue,
			wantReason:  "生产事故热修复",
		},
		{
			name:        "continue marker without reason",
			input:       codexPromptInput("$walkout:continue"),
			wantMatch:   true,
			wantCommand: protocol.CommandEmergencyContinue,
		},
		{
			name:        "horizontal spacing before reason",
			input:       codexPromptInput("$walkout:continue\t  incident response"),
			wantMatch:   true,
			wantCommand: protocol.CommandEmergencyContinue,
			wantReason:  "incident response",
		},
		{
			name:        "done marker",
			input:       codexPromptInput("$walkout:done"),
			wantMatch:   true,
			wantCommand: protocol.CommandConfirmActivity,
		},
		{
			name:  "done does not accept arguments",
			input: codexPromptInput("$walkout:done please"),
		},
		{
			name:  "done token boundary mismatch",
			input: codexPromptInput("$walkout:done-now"),
		},
		{
			name:  "ordinary work prompt",
			input: codexPromptInput("CONFIDENTIAL_CLIENT_WORK"),
		},
		{
			name:  "leading whitespace",
			input: codexPromptInput(" $walkout:continue incident"),
		},
		{
			name:  "case mismatch",
			input: codexPromptInput("$Walkout:continue incident"),
		},
		{
			name:  "token boundary mismatch",
			input: codexPromptInput("$walkout:continue-now incident"),
		},
		{
			name:  "marker after work content",
			input: codexPromptInput("fix the bug $walkout:continue incident"),
		},
		{
			name:  "multiline remainder",
			input: codexPromptInput("$walkout:continue incident\nthen inspect secrets"),
		},
		{
			name:  "non prompt event",
			input: `{"session_id":"s","hook_event_name":"SessionStart","source":"startup"}`,
		},
		{
			name:    "malformed JSON",
			input:   `{`,
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request, matched, err := ParseControlRequest([]byte(test.input))
			if (err != nil) != test.wantErr {
				t.Fatalf("ParseControlRequest() error = %v, wantErr %t", err, test.wantErr)
			}
			if matched != test.wantMatch {
				t.Fatalf("ParseControlRequest() matched = %t, want %t", matched, test.wantMatch)
			}
			if request.Command != test.wantCommand {
				t.Fatalf("ParseControlRequest() command = %q, want %q", request.Command, test.wantCommand)
			}
			if request.Reason != test.wantReason {
				t.Fatalf("ParseControlRequest() reason = %q, want %q", request.Reason, test.wantReason)
			}
			if !matched && request != (ControlRequest{}) {
				t.Fatalf("non-match returned data: %#v", request)
			}
		})
	}
}

func codexPromptInput(prompt string) string {
	replacer := strings.NewReplacer("\\", "\\\\", `"`, `\"`, "\n", "\\n", "\r", "\\r", "\t", "\\t")
	return `{"session_id":"s","turn_id":"t","hook_event_name":"UserPromptSubmit","prompt":"` + replacer.Replace(prompt) + `"}`
}
