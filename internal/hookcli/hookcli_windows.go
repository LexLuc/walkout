//go:build windows

package hookcli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/LexLuc/walkout/internal/adapters"
	"github.com/LexLuc/walkout/internal/adapters/claudecode"
	"github.com/LexLuc/walkout/internal/adapters/codex"
	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/protocol"
	"github.com/LexLuc/walkout/internal/transport/localipc"
	"github.com/LexLuc/walkout/internal/transport/ndjson"
)

// DefaultMaxInputBytes bounds the host hook payload read from stdin; the
// prompt body can be large, but anything past this is treated as fail-open
// rather than buffered without limit.
const DefaultMaxInputBytes = 4 * 1024 * 1024

// eventProbeOutputEnv is set only by the versioned real-host probe. The
// production plugin does not set it. When enabled, the hook records four
// non-sensitive fields from the final translated event after the daemon has
// accepted it; host input, session IDs, cwd, and payload are never written.
const eventProbeOutputEnv = "WALKOUT_EVENT_PROBE_OUTPUT"

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type randomIDs struct{}

func (randomIDs) NewID() string {
	var buffer [16]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		// Translators reject an empty ID, which surfaces as silent fail-open.
		return ""
	}
	return hex.EncodeToString(buffer[:])
}

// Run executes one hook invocation: stdin → host translator → daemon
// process_event → host renderer → stdout/exit code. Every failure on this
// path is a silent fail-open so the host session is never disturbed.
func Run(ctx context.Context, arguments []string, stdin io.Reader, stdout, stderr io.Writer) int {
	result := execute(ctx, arguments, stdin)
	if len(result.Stdout) > 0 {
		_, _ = stdout.Write(result.Stdout)
	}
	if len(result.Stderr) > 0 {
		_, _ = stderr.Write(result.Stderr)
	}
	return result.ExitCode
}

func execute(ctx context.Context, arguments []string, stdin io.Reader) adapters.HookResult {
	flags := flag.NewFlagSet("walkout-hook", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	provider := flags.String("provider", "", "host provider: codex or claude-code")
	hostVersion := flags.String("host-version", "", "host application version")
	pipeName := flags.String("pipe", "", "walkoutd named pipe (default: current-user pipe)")
	timeout := flags.Duration("timeout", localipc.DefaultTimeout, "end-to-end daemon call deadline")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return failOpen()
	}

	runner, err := buildRunner(*provider, *hostVersion, *pipeName, *timeout, os.Getenv(eventProbeOutputEnv))
	if err != nil {
		return failOpen()
	}
	input, err := readBoundedInput(stdin, DefaultMaxInputBytes)
	if err != nil {
		return failOpen()
	}
	return runner.Handle(ctx, input)
}

type hookRunner struct {
	provider string
	runner   *adapters.Runner
	controls daemonClient
}

func (r *hookRunner) Handle(ctx context.Context, input []byte) adapters.HookResult {
	if r.provider == "codex" {
		request, matched, err := codex.ParseControlRequest(input)
		if err == nil && matched {
			// A rejected or unavailable control request must still flow through the
			// ordinary prompt decision. If the daemon is healthy it remains paused;
			// if it is unavailable the existing runner policy fails open.
			_ = r.controls.ExecuteControl(ctx, request.Command, request.Reason)
		}
	}
	return r.runner.Handle(ctx, input)
}

func buildRunner(provider, hostVersion, pipeName string, timeout time.Duration, eventProbePath string) (*hookRunner, error) {
	var (
		translator adapters.Translator
		renderer   adapters.Renderer
		err        error
	)
	switch provider {
	case "codex":
		translator, err = codex.NewTranslator(codex.Config{
			HostVersion:  hostVersion,
			Capabilities: []protocol.Capability{protocol.CapabilityBlockPrompt, protocol.CapabilityInjectContext},
		}, systemClock{}, randomIDs{})
		codexRenderer := codex.NewRenderer()
		codexRenderer.CtlCommand = bundledCtlCommand()
		renderer = codexRenderer
	case "claude-code":
		translator, err = claudecode.NewTranslator(claudecode.Config{
			HostVersion:  hostVersion,
			Capabilities: []protocol.Capability{protocol.CapabilityBlockPrompt, protocol.CapabilityInjectContext},
		}, systemClock{}, randomIDs{})
		claudeRenderer := claudecode.NewRenderer()
		claudeRenderer.CtlCommand = bundledCtlCommand()
		renderer = claudeRenderer
	default:
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
	if err != nil {
		return nil, err
	}
	if pipeName == "" {
		identity, err := localipc.CurrentUserIdentity()
		if err != nil {
			return nil, err
		}
		pipeName = identity.PipeName
	}
	client := daemonClient{
		client:         localipc.Client{PipeName: pipeName, Timeout: timeout},
		ids:            randomIDs{},
		eventProbePath: eventProbePath,
	}
	runner, err := adapters.NewRunner(translator, client, renderer)
	if err != nil {
		return nil, err
	}
	return &hookRunner{provider: provider, runner: runner, controls: client}, nil
}

// bundledCtlCommand resolves the walkout-ctl.exe that ships beside this hook
// binary so injected instructions can name an executable path that works
// without PATH changes. The bare name is the fallback if resolution fails.
func bundledCtlCommand() string {
	self, err := os.Executable()
	if err != nil {
		return "walkout-ctl"
	}
	candidate := filepath.Join(filepath.Dir(self), "walkout-ctl.exe")
	if _, err := os.Stat(candidate); err != nil {
		return "walkout-ctl"
	}
	return candidate
}

type daemonClient struct {
	client         localipc.Client
	ids            randomIDs
	eventProbePath string
}

func (c daemonClient) Decide(ctx context.Context, event protocol.HealthEvent) (protocol.HealthDecision, error) {
	encoded, err := json.Marshal(ndjson.ProcessEventPayload{
		Event:            event,
		AssessmentStatus: string(core.AssessmentMissing),
	})
	if err != nil {
		return protocol.HealthDecision{}, err
	}
	requestID := c.ids.NewID()
	if requestID == "" {
		return protocol.HealthDecision{}, errors.New("request ID generation failed")
	}
	response, err := c.client.Call(ctx, ndjson.Request{
		SchemaVersion: protocol.SchemaVersion,
		RequestID:     requestID,
		Operation:     ndjson.OperationProcessEvent,
		Payload:       encoded,
	})
	if err != nil {
		return protocol.HealthDecision{}, err
	}
	if !response.OK || response.Error != nil {
		return protocol.HealthDecision{}, errors.New("daemon rejected the event")
	}
	var decision protocol.HealthDecision
	if err := json.Unmarshal(response.Payload, &decision); err != nil {
		return protocol.HealthDecision{}, err
	}
	appendEventProbe(c.eventProbePath, event)
	return decision, nil
}

type eventProbeRecord struct {
	SchemaVersion string             `json:"schema_version"`
	Provider      protocol.Provider  `json:"provider"`
	HostVersion   string             `json:"host_version"`
	EventType     protocol.EventType `json:"event_type"`
}

func appendEventProbe(path string, event protocol.HealthEvent) {
	if path == "" {
		return
	}
	record, err := json.Marshal(eventProbeRecord{
		SchemaVersion: event.SchemaVersion,
		Provider:      event.Provider,
		HostVersion:   event.HostVersion,
		EventType:     event.EventType,
	})
	if err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	_, _ = file.Write(append(record, '\n'))
	_ = file.Close()
}

// ExecuteControl sends one host-neutral control command to the daemon. The
// start_break command carries the fixed V1 generic-break activity; only
// emergency_continue carries the optional reason.
func (c daemonClient) ExecuteControl(ctx context.Context, commandType protocol.HealthControlCommandType, reason string) error {
	commandID := c.ids.NewID()
	if commandID == "" {
		return errors.New("command ID generation failed")
	}
	command := protocol.HealthControlCommand{
		SchemaVersion: protocol.SchemaVersion,
		CommandID:     commandID,
		CommandType:   commandType,
	}
	if commandType == protocol.CommandEmergencyContinue {
		command.Reason = reason
	}
	payload, err := json.Marshal(command)
	if err != nil {
		return err
	}
	requestID := c.ids.NewID()
	if requestID == "" {
		return errors.New("request ID generation failed")
	}
	response, err := c.client.Call(ctx, ndjson.Request{
		SchemaVersion: protocol.SchemaVersion,
		RequestID:     requestID,
		Operation:     ndjson.OperationExecuteCommand,
		Payload:       payload,
	})
	if err != nil {
		return err
	}
	if !response.OK || response.Error != nil {
		return errors.New("daemon rejected the control command")
	}
	var controlResponse protocol.HealthControlResponse
	if err := json.Unmarshal(response.Payload, &controlResponse); err != nil {
		return err
	}
	if controlResponse.CommandID != commandID ||
		controlResponse.CommandType != commandType ||
		!protocol.IsSupportedSchema(controlResponse.SchemaVersion) {
		return errors.New("daemon returned an invalid control response")
	}
	return nil
}

func readBoundedInput(stdin io.Reader, limit int64) ([]byte, error) {
	input, err := io.ReadAll(io.LimitReader(stdin, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(input)) > limit {
		return nil, errors.New("hook input exceeds size limit")
	}
	return input, nil
}

func failOpen() adapters.HookResult {
	return adapters.HookResult{ExitCode: 0, FailOpen: true}
}
