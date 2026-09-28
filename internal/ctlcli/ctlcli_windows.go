//go:build windows

package ctlcli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/LexLuc/walkout/internal/protocol"
	"github.com/LexLuc/walkout/internal/transport/localipc"
	"github.com/LexLuc/walkout/internal/transport/ndjson"
)

// Run drives one user control command against walkoutd: it builds a
// HealthControlCommand, calls the execute_command RPC, and renders the
// resulting health status. Unlike the hook path, the control plane is a
// deliberate user action, so failures surface as a visible message and a
// non-zero exit code rather than a silent fail-open.
func Run(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	if len(arguments) == 0 {
		fmt.Fprintln(stderr, usage())
		return 2
	}
	subcommand := arguments[0]

	// status is the only read-only verb; every other verb mutates state through
	// execute_command. Both are rejected early if the verb is unknown.
	isStatus := subcommand == "status"
	commandType, ok := resolveCommand(subcommand)
	if !isStatus && !ok {
		fmt.Fprintf(stderr, "unknown command %q\n%s\n", subcommand, usage())
		return 2
	}

	flags := flag.NewFlagSet("walkout-ctl "+subcommand, flag.ContinueOnError)
	flags.SetOutput(stderr)
	pipeName := flags.String("pipe", "", "walkoutd named pipe (default: current-user pipe)")
	timeout := flags.Duration("timeout", localipc.DefaultTimeout, "end-to-end daemon call deadline")
	reason := flags.String("reason", "", "optional note recorded with emergency-continue")
	if err := flags.Parse(arguments[1:]); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "unexpected arguments: %v\n%s\n", flags.Args(), usage())
		return 2
	}

	var (
		status protocol.HealthStatus
		err    error
	)
	if isStatus {
		status, err = queryStatus(ctx, *pipeName, *timeout)
	} else {
		status, err = execute(ctx, commandType, *reason, *pipeName, *timeout)
	}
	if err != nil {
		fmt.Fprintf(stderr, "walkout-ctl: %v\n", err)
		return 1
	}
	renderStatus(stdout, status)
	return 0
}

func queryStatus(ctx context.Context, pipeName string, timeout time.Duration) (protocol.HealthStatus, error) {
	if pipeName == "" {
		identity, err := localipc.CurrentUserIdentity()
		if err != nil {
			return protocol.HealthStatus{}, err
		}
		pipeName = identity.PipeName
	}
	requestID, err := newID()
	if err != nil {
		return protocol.HealthStatus{}, err
	}
	client := localipc.Client{PipeName: pipeName, Timeout: timeout}
	response, err := client.Call(ctx, ndjson.Request{
		SchemaVersion: protocol.SchemaVersion,
		RequestID:     requestID,
		Operation:     ndjson.OperationStatus,
		Payload:       json.RawMessage(`{}`),
	})
	if err != nil {
		return protocol.HealthStatus{}, fmt.Errorf("walkoutd is unavailable: %w", err)
	}
	if !response.OK || response.Error != nil {
		return protocol.HealthStatus{}, commandError(response.Error)
	}
	var status protocol.HealthStatus
	if err := json.Unmarshal(response.Payload, &status); err != nil {
		return protocol.HealthStatus{}, fmt.Errorf("daemon returned an unreadable status: %w", err)
	}
	return status, nil
}

func execute(
	ctx context.Context,
	commandType protocol.HealthControlCommandType,
	reason string,
	pipeName string,
	timeout time.Duration,
) (protocol.HealthStatus, error) {
	commandID, err := newID()
	if err != nil {
		return protocol.HealthStatus{}, err
	}
	if pipeName == "" {
		identity, err := localipc.CurrentUserIdentity()
		if err != nil {
			return protocol.HealthStatus{}, err
		}
		pipeName = identity.PipeName
	}

	command := protocol.HealthControlCommand{
		SchemaVersion: protocol.SchemaVersion,
		CommandID:     commandID,
		CommandType:   commandType,
		Reason:        reason,
	}
	payload, err := json.Marshal(command)
	if err != nil {
		return protocol.HealthStatus{}, err
	}
	requestID, err := newID()
	if err != nil {
		return protocol.HealthStatus{}, err
	}

	client := localipc.Client{PipeName: pipeName, Timeout: timeout}
	response, err := client.Call(ctx, ndjson.Request{
		SchemaVersion: protocol.SchemaVersion,
		RequestID:     requestID,
		Operation:     ndjson.OperationExecuteCommand,
		Payload:       payload,
	})
	if err != nil {
		return protocol.HealthStatus{}, fmt.Errorf("walkoutd is unavailable: %w", err)
	}
	if !response.OK || response.Error != nil {
		return protocol.HealthStatus{}, commandError(response.Error)
	}

	var controlResponse protocol.HealthControlResponse
	if err := json.Unmarshal(response.Payload, &controlResponse); err != nil {
		return protocol.HealthStatus{}, fmt.Errorf("daemon returned an unreadable response: %w", err)
	}
	return controlResponse.Status, nil
}

func resolveCommand(subcommand string) (protocol.HealthControlCommandType, bool) {
	switch subcommand {
	case "done":
		return protocol.CommandConfirmActivity, true
	case "emergency-continue":
		return protocol.CommandEmergencyContinue, true
	default:
		return "", false
	}
}

func commandError(rpcErr *ndjson.RPCError) error {
	if rpcErr == nil {
		return errors.New("daemon rejected the command")
	}
	switch rpcErr.Code {
	case ndjson.ErrorBusinessRejected:
		return errors.New("the command is not available in the current health state")
	default:
		return fmt.Errorf("daemon rejected the command (%s)", rpcErr.Code)
	}
}

func renderStatus(stdout io.Writer, status protocol.HealthStatus) {
	fmt.Fprintf(stdout, "state: %s\n", status.State)
	fmt.Fprintf(stdout, "state_revision: %d\n", status.StateRevision)
	fmt.Fprintf(stdout, "continuous_work_seconds: %d\n", status.ContinuousWorkSeconds)
	fmt.Fprintf(stdout, "health_debt_seconds: %d\n", status.HealthDebtSeconds)
	fmt.Fprintf(stdout, "activity_count: %d\n", status.ActivityCount)
	fmt.Fprintf(stdout, "emergency_continue_active: %t\n", status.EmergencyContinueActive)
	fmt.Fprintf(stdout, "emergency_continue_remaining_seconds: %d\n", status.EmergencyContinueRemainingSecond)
	fmt.Fprintf(stdout, "emergency_continue_reason: %s\n", status.EmergencyContinueReason)
}

func usage() string {
	return "usage: walkout-ctl <status|done|emergency-continue> [-reason text] [-pipe name] [-timeout duration]"
}

func newID() (string, error) {
	var buffer [16]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return "", errors.New("failed to generate a command ID")
	}
	return hex.EncodeToString(buffer[:]), nil
}
