//go:build windows

package hookcli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LexLuc/walkout/internal/application"
	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/protocol"
	"github.com/LexLuc/walkout/internal/transport/localipc"
	"github.com/LexLuc/walkout/internal/transport/ndjson"
)

var hookPipeSequence atomic.Int64

func hookPipeName() string {
	return fmt.Sprintf(
		`\\.\pipe\walkout-hookcli-test-%d-%d`,
		os.Getpid(),
		hookPipeSequence.Add(1),
	)
}

type fakeService struct {
	action protocol.DecisionAction
	reason protocol.ReasonCode
}

func (s fakeService) ProcessEvent(
	_ context.Context,
	event protocol.HealthEvent,
	_ core.AssessmentStatus,
) (protocol.HealthDecision, error) {
	return protocol.HealthDecision{
		SchemaVersion: protocol.SchemaVersion,
		DecisionID:    "decision-1",
		SourceEventID: event.EventID,
		State:         protocol.StateWalkout,
		Action:        s.action,
		ReasonCode:    s.reason,
	}, nil
}

func (s fakeService) Status(context.Context) (protocol.HealthStatus, error) {
	return protocol.HealthStatus{}, errors.New("not supported in hook CLI tests")
}

func (s fakeService) ExecuteCommand(
	context.Context,
	protocol.HealthControlCommand,
) (protocol.HealthControlResponse, error) {
	return protocol.HealthControlResponse{}, errors.New("not supported in hook CLI tests")
}

func startDaemonPipe(t *testing.T, pipeName string, service ndjson.Service) {
	t.Helper()
	handler, err := ndjson.NewHandler(service, 0)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	server, err := localipc.NewServer(localipc.Config{PipeName: pipeName}, handler)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	go func() { _ = server.Serve(context.Background()) }()
	t.Cleanup(func() { _ = server.Close() })
}

func hostArguments(provider, hostVersion, pipeName string) []string {
	return []string{
		"-provider", provider,
		"-host-version", hostVersion,
		"-pipe", pipeName,
		"-timeout", "2s",
	}
}

func readHostFixture(t *testing.T, host, name string) []byte {
	t.Helper()
	var fixturePath string
	switch host {
	case "claude-code":
		fixturePath = filepath.Join("..", "adapters", "claudecode", "testdata", "v2.1.224", name)
	case "codex":
		fixturePath = filepath.Join("..", "adapters", "codex", "testdata", "v0.147.0", name)
	default:
		t.Fatalf("unknown fixture host %q", host)
	}
	encoded, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture error = %v", err)
	}
	return encoded
}

func TestRunRendersPromptPauseFromRecordedFixturesOverRealPipe(t *testing.T) {
	for _, host := range []struct {
		provider    string
		hostVersion string
	}{
		{"claude-code", "2.1.224"},
		{"codex", "0.147.0"},
	} {
		t.Run(host.provider, func(t *testing.T) {
			pipeName := hookPipeName()
			startDaemonPipe(t, pipeName, fakeService{
				action: protocol.ActionPausePrompt,
				reason: protocol.ReasonBreakDue,
			})

			var stdout, stderr bytes.Buffer
			code := Run(
				context.Background(),
				hostArguments(host.provider, host.hostVersion, pipeName),
				bytes.NewReader(readHostFixture(t, host.provider, "user_prompt_submit.json")),
				&stdout,
				&stderr,
			)
			if code != 0 || stderr.Len() != 0 {
				t.Fatalf("exit/stderr = %d/%q, want 0 and empty", code, stderr.String())
			}
			var blockOutput struct {
				Decision string `json:"decision"`
				Reason   string `json:"reason"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &blockOutput); err != nil {
				t.Fatalf("stdout %q is not valid JSON: %v", stdout.String(), err)
			}
			if blockOutput.Decision != "block" || blockOutput.Reason == "" {
				t.Fatalf("block output = %#v", blockOutput)
			}
		})
	}
}

func TestRunStaysSilentWhenDecisionIsAllow(t *testing.T) {
	pipeName := hookPipeName()
	startDaemonPipe(t, pipeName, fakeService{
		action: protocol.ActionAllow,
		reason: protocol.ReasonNotDue,
	})

	var stdout, stderr bytes.Buffer
	code := Run(
		context.Background(),
		hostArguments("claude-code", "2.1.224", pipeName),
		bytes.NewReader(readHostFixture(t, "claude-code", "user_prompt_submit.json")),
		&stdout,
		&stderr,
	)
	if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("exit/stdout/stderr = %d/%q/%q, want silent success", code, stdout.String(), stderr.String())
	}
}

func TestRunRecordsOnlySanitizedFinalEventMetadataWhenProbeIsEnabled(t *testing.T) {
	tests := []struct {
		name            string
		hostVersion     string
		wantHostVersion string
	}{
		{name: "managed package version", hostVersion: "0.146.0", wantHostVersion: "0.146.0"},
		{name: "standalone fallback", hostVersion: "", wantHostVersion: "unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pipeName := hookPipeName()
			startDaemonPipe(t, pipeName, fakeService{
				action: protocol.ActionAllow,
				reason: protocol.ReasonNotDue,
			})

			probePath := filepath.Join(t.TempDir(), "health-events.jsonl")
			t.Setenv(eventProbeOutputEnv, probePath)
			input := withHostPrompt(
				t,
				readHostFixture(t, "codex", "user_prompt_submit.json"),
				"CONFIDENTIAL_CLIENT_MATTER",
			)
			var stdout, stderr bytes.Buffer
			code := Run(
				context.Background(),
				hostArguments("codex", test.hostVersion, pipeName),
				bytes.NewReader(input),
				&stdout,
				&stderr,
			)
			if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("exit/stdout/stderr = %d/%q/%q, want silent success", code, stdout.String(), stderr.String())
			}

			encoded, err := os.ReadFile(probePath)
			if err != nil {
				t.Fatalf("read event probe output: %v", err)
			}
			if bytes.Contains(encoded, []byte("CONFIDENTIAL_CLIENT_MATTER")) || bytes.Contains(encoded, []byte("session-fixture")) {
				t.Fatalf("event probe leaked host content: %s", encoded)
			}
			var record map[string]string
			if err := json.Unmarshal(bytes.TrimSpace(encoded), &record); err != nil {
				t.Fatalf("decode event probe output %q: %v", encoded, err)
			}
			if len(record) != 4 {
				t.Fatalf("event probe fields = %#v, want four sanitized fields", record)
			}
			if record["schema_version"] != protocol.SchemaVersion ||
				record["provider"] != string(protocol.ProviderCodex) ||
				record["host_version"] != test.wantHostVersion ||
				record["event_type"] != string(protocol.EventTypePromptSubmit) {
				t.Fatalf("event probe record = %#v", record)
			}
		})
	}
}

func TestRunIgnoresEventProbeWriteFailure(t *testing.T) {
	pipeName := hookPipeName()
	startDaemonPipe(t, pipeName, fakeService{
		action: protocol.ActionAllow,
		reason: protocol.ReasonNotDue,
	})
	t.Setenv(eventProbeOutputEnv, filepath.Join(t.TempDir(), "missing", "health-events.jsonl"))

	var stdout, stderr bytes.Buffer
	code := Run(
		context.Background(),
		hostArguments("codex", "0.146.0", pipeName),
		bytes.NewReader(readHostFixture(t, "codex", "user_prompt_submit.json")),
		&stdout,
		&stderr,
	)
	if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("exit/stdout/stderr = %d/%q/%q, want probe failure to preserve silent success", code, stdout.String(), stderr.String())
	}
}

type hookFakeClock struct {
	now time.Time
}

func (c *hookFakeClock) Now() time.Time {
	return c.now
}

func TestRunCodexEmergencyContinueAuthorizesTheSamePromptOverRealPipe(t *testing.T) {
	clock := &hookFakeClock{now: time.Now().UTC().Add(-time.Minute)}
	service := newPausedHookService(t, clock)
	pipeName := hookPipeName()
	startDaemonPipe(t, pipeName, service)

	var stdout, stderr bytes.Buffer
	code := Run(
		context.Background(),
		hostArguments("codex", "0.146.0", pipeName),
		bytes.NewReader(readCodexFixtureVersion(t, "v0.146.0", "user_prompt_submit_emergency_continue.json")),
		&stdout,
		&stderr,
	)
	if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("exit/stdout/stderr = %d/%q/%q, want same-submit silent allow", code, stdout.String(), stderr.String())
	}

	clock.now = clock.now.Add(2 * time.Minute)
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.State != protocol.StateWalkout || !status.EmergencyContinueActive {
		t.Fatalf("status = %#v, want paused state with active emergency lease", status)
	}
	if status.EmergencyContinueReason != "生产事故热修复" {
		t.Fatalf("EmergencyContinueReason = %q", status.EmergencyContinueReason)
	}

	clock.now = clock.now.Add(16 * time.Minute)
	stdout.Reset()
	code = Run(
		context.Background(),
		hostArguments("codex", "0.147.0", pipeName),
		bytes.NewReader(readHostFixture(t, "codex", "user_prompt_submit.json")),
		&stdout,
		&stderr,
	)
	if code != 0 || stderr.Len() != 0 {
		t.Fatalf("expired lease exit/stderr = %d/%q", code, stderr.String())
	}
	assertBlockedOutput(t, stdout.Bytes())
}

func TestRunCodexDoneMarkerConfirmsActivityOverRealPipe(t *testing.T) {
	clock := &hookFakeClock{now: time.Now().UTC()}
	config := core.DefaultConfig()
	config.WorkInterval = 2 * time.Minute

	service, err := application.NewService(context.Background(), config, clock, core.NewPolicy(), application.NewMemoryResultStore())
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	pipeName := hookPipeName()
	startDaemonPipe(t, pipeName, service)

	// Prime the reminder phase with two interactions three minutes apart; the
	// hook-submitted markers below then carry near-now timestamps consistently.
	for i, occurredAt := range []time.Time{clock.now.Add(-3 * time.Minute), clock.now.Add(-time.Minute)} {
		if _, err := service.ProcessEvent(context.Background(), protocol.HealthEvent{
			SchemaVersion: protocol.SchemaVersion,
			EventID:       fmt.Sprintf("nudge-prime-%d", i),
			Provider:      protocol.ProviderCodex,
			HostVersion:   "0.147.0",
			SessionID:     "prime-session",
			EventType:     protocol.EventTypePromptSubmit,
			OccurredAt:    occurredAt,
			ActivityKind:  protocol.ActivityKindHumanInput,
			Capabilities:  []protocol.Capability{protocol.CapabilityBlockPrompt},
			Payload:       map[string]json.RawMessage{},
		}, core.AssessmentMissing); err != nil {
			t.Fatalf("prime event %d error = %v", i, err)
		}
	}

	submitMarker := func(marker string) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := Run(
			context.Background(),
			hostArguments("codex", "0.146.0", pipeName),
			bytes.NewReader(withHostPrompt(t, readHostFixture(t, "codex", "user_prompt_submit.json"), marker)),
			&stdout,
			&stderr,
		)
		if code != 0 || stderr.Len() != 0 {
			t.Fatalf("%s exit/stderr = %d/%q", marker, code, stderr.String())
		}
	}
	stateNow := func() protocol.HealthStatus {
		t.Helper()
		// Hook-submitted events carry real wall-clock timestamps, so the fake
		// clock must track real time for the status restore; it must never move
		// ahead of real time, or control-side ticks would stamp future instants
		// and reject the next hook event.
		clock.now = time.Now().UTC()
		status, err := service.Status(context.Background())
		if err != nil {
			t.Fatalf("Status() error = %v", err)
		}
		return status
	}

	// The primed interactions put the engine into overtime.
	if got := stateNow(); got.State != protocol.StateOvertime {
		t.Fatalf("primed state = %q, want overtime", got.State)
	}

	// $walkout:done confirms the activity in one step: the control runs before
	// the same submission's event, clearing work and debt back to working.
	submitMarker("$walkout:done")
	status := stateNow()
	if status.State != protocol.StateWorking {
		t.Fatalf("state after done marker = %q, want working", status.State)
	}
	if status.ActivityCount != 1 || status.HealthDebtSeconds != 0 {
		t.Fatalf("status after done marker = %#v, want one confirmed activity and no debt", status)
	}
}

func readCodexFixtureVersion(t *testing.T, version, name string) []byte {
	t.Helper()
	encoded, err := os.ReadFile(filepath.Join("..", "adapters", "codex", "testdata", version, name))
	if err != nil {
		t.Fatalf("read Codex fixture error = %v", err)
	}
	return encoded
}

func TestRunDoesNotAuthorizeNonControlPrompts(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		version  string
		input    []byte
	}{
		{
			name:     "ordinary Codex work prompt",
			provider: "codex",
			version:  "0.147.0",
			input:    readHostFixture(t, "codex", "user_prompt_submit.json"),
		},
		{
			name:     "Codex marker after leading whitespace",
			provider: "codex",
			version:  "0.147.0",
			input:    withHostPrompt(t, readHostFixture(t, "codex", "user_prompt_submit.json"), " $walkout:continue incident"),
		},
		{
			name:     "Codex multiline remainder",
			provider: "codex",
			version:  "0.147.0",
			input:    withHostPrompt(t, readHostFixture(t, "codex", "user_prompt_submit.json"), "$walkout:continue incident\nthen inspect work"),
		},
		{
			name:     "Codex reason beyond the existing protocol limit",
			provider: "codex",
			version:  "0.147.0",
			input:    withHostPrompt(t, readHostFixture(t, "codex", "user_prompt_submit.json"), "$walkout:continue "+strings.Repeat("界", 257)),
		},
		{
			name:     "Claude Code remains host-specific",
			provider: "claude-code",
			version:  "2.1.224",
			input:    withHostPrompt(t, readHostFixture(t, "claude-code", "user_prompt_submit.json"), "$walkout:continue incident"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := &hookFakeClock{now: time.Now().UTC().Add(-time.Minute)}
			service := newPausedHookService(t, clock)
			pipeName := hookPipeName()
			startDaemonPipe(t, pipeName, service)

			var stdout, stderr bytes.Buffer
			code := Run(
				context.Background(),
				hostArguments(test.provider, test.version, pipeName),
				bytes.NewReader(test.input),
				&stdout,
				&stderr,
			)
			if code != 0 || stderr.Len() != 0 {
				t.Fatalf("exit/stderr = %d/%q", code, stderr.String())
			}
			assertBlockedOutput(t, stdout.Bytes())

			clock.now = clock.now.Add(2 * time.Minute)
			status, err := service.Status(context.Background())
			if err != nil {
				t.Fatalf("Status() error = %v", err)
			}
			if status.EmergencyContinueActive || status.EmergencyContinueReason != "" {
				t.Fatalf("non-control prompt granted emergency lease: %#v", status)
			}
		})
	}
}

func newPausedHookService(t *testing.T, clock *hookFakeClock) *application.Service {
	t.Helper()
	config := core.DefaultConfig()
	config.WorkInterval = 2 * time.Minute

	config.DebtLimit = 5 * time.Minute
	store := application.NewMemoryResultStore()
	service, err := application.NewService(context.Background(), config, clock, core.NewPolicy(), store)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}

	start := clock.now.Add(-16 * time.Minute)
	for i := range 5 {
		occurredAt := start.Add(time.Duration(i) * 4 * time.Minute)
		_, err := service.ProcessEvent(context.Background(), protocol.HealthEvent{
			SchemaVersion: protocol.SchemaVersion,
			EventID:       fmt.Sprintf("prime-%d", i),
			Provider:      protocol.ProviderCodex,
			HostVersion:   "0.147.0",
			SessionID:     "prime-session",
			EventType:     protocol.EventTypePromptSubmit,
			OccurredAt:    occurredAt,
			ActivityKind:  protocol.ActivityKindHumanInput,
			Capabilities:  []protocol.Capability{protocol.CapabilityBlockPrompt},
			Payload:       map[string]json.RawMessage{},
		}, core.AssessmentMissing)
		if err != nil {
			t.Fatalf("prime event %d error = %v", i, err)
		}
	}
	status, err := service.Status(context.Background())
	if err != nil || status.State != protocol.StateWalkout {
		t.Fatalf("primed status = %#v, %v; want service_paused", status, err)
	}
	return service
}

func withHostPrompt(t *testing.T, input []byte, prompt string) []byte {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(input, &payload); err != nil {
		t.Fatalf("fixture decode error = %v", err)
	}
	payload["prompt"] = prompt
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("fixture encode error = %v", err)
	}
	return encoded
}

func assertBlockedOutput(t *testing.T, encoded []byte) {
	t.Helper()
	var output struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := json.Unmarshal(encoded, &output); err != nil {
		t.Fatalf("block output %q is invalid: %v", encoded, err)
	}
	if output.Decision != "block" || output.Reason == "" {
		t.Fatalf("block output = %#v", output)
	}
}

func TestRunFailsOpenWhenDaemonIsUnavailable(t *testing.T) {
	started := time.Now()
	var stdout, stderr bytes.Buffer
	code := Run(
		context.Background(),
		[]string{
			"-provider", "claude-code",
			"-host-version", "2.1.224",
			"-pipe", hookPipeName(),
		},
		bytes.NewReader(readHostFixture(t, "claude-code", "user_prompt_submit.json")),
		&stdout,
		&stderr,
	)
	if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("exit/stdout/stderr = %d/%q/%q, want silent fail-open", code, stdout.String(), stderr.String())
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("fail-open took %v, want prompt return under the hook budget", elapsed)
	}
}

func TestRunFailsOpenOnInvalidArgumentsOrInput(t *testing.T) {
	pipeName := hookPipeName()
	tests := []struct {
		name      string
		arguments []string
		stdin     []byte
	}{
		{"unknown provider", hostArguments("workbuddy", "5.3.5", pipeName), readHostFixture(t, "claude-code", "user_prompt_submit.json")},
		{"missing host version", []string{"-provider", "claude-code", "-pipe", pipeName}, readHostFixture(t, "claude-code", "user_prompt_submit.json")},
		{"unknown flag", []string{"-provider", "claude-code", "-host-version", "2.1.224", "-unexpected"}, nil},
		{"malformed stdin", hostArguments("claude-code", "2.1.224", pipeName), []byte(`{`)},
		{"unsupported hook event", hostArguments("claude-code", "2.1.224", pipeName), []byte(`{"session_id":"s","hook_event_name":"FutureEvent"}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), test.arguments, bytes.NewReader(test.stdin), &stdout, &stderr)
			if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("exit/stdout/stderr = %d/%q/%q, want silent fail-open", code, stdout.String(), stderr.String())
			}
		})
	}
}
