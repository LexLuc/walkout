//go:build windows

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LexLuc/walkout/internal/protocol"
	"github.com/LexLuc/walkout/internal/transport/localipc"
	"github.com/LexLuc/walkout/internal/transport/ndjson"
)

var daemonPipeSequence atomic.Uint64

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func TestDaemonComposesSQLiteRPCAndRestoresStateAfterRestart(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "state", "walkout.db")
	pipeName := daemonPipeName()
	clock := fixedClock{now: time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)}

	first, err := New(context.Background(), Config{
		DatabasePath: databasePath,
		PipeName:     pipeName,
	}, clock)
	if err != nil {
		t.Fatalf("first New() error = %v", err)
	}
	firstDone := serveDaemon(first)
	client := localipc.Client{PipeName: pipeName}

	commandPayload := mustJSON(t, protocol.HealthControlCommand{
		SchemaVersion: protocol.SchemaVersion,
		CommandID:     "confirm-activity",
		CommandType:   protocol.CommandConfirmActivity,
	})
	response, err := client.Call(context.Background(), ndjson.Request{
		SchemaVersion: protocol.SchemaVersion,
		RequestID:     "confirm-activity",
		Operation:     ndjson.OperationExecuteCommand,
		Payload:       commandPayload,
	})
	if err != nil {
		t.Fatalf("execute command Call() error = %v", err)
	}
	var control protocol.HealthControlResponse
	if err := json.Unmarshal(response.Payload, &control); err != nil {
		t.Fatalf("decode control response error = %v", err)
	}
	if control.Status.State != protocol.StateWorking || control.Status.ActivityCount != 1 {
		t.Fatalf("command status = %#v, want working with one confirmed activity", control.Status)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	waitDaemon(t, firstDone)

	second, err := New(context.Background(), Config{
		DatabasePath: databasePath,
		PipeName:     pipeName,
	}, clock)
	if err != nil {
		t.Fatalf("second New() error = %v", err)
	}
	secondDone := serveDaemon(second)
	t.Cleanup(func() {
		if err := second.Close(); err != nil {
			t.Errorf("second Close() error = %v", err)
		}
		waitDaemon(t, secondDone)
	})

	statusResponse, err := client.Call(context.Background(), ndjson.Request{
		SchemaVersion: protocol.SchemaVersion,
		RequestID:     "status-after-restart",
		Operation:     ndjson.OperationStatus,
		Payload:       mustJSON(t, struct{}{}),
	})
	if err != nil {
		t.Fatalf("status Call() error = %v", err)
	}
	var status protocol.HealthStatus
	if err := json.Unmarshal(statusResponse.Payload, &status); err != nil {
		t.Fatalf("decode status error = %v", err)
	}
	if status.State != protocol.StateWorking || status.ActivityCount != 1 {
		t.Fatalf("restored status = %#v, want working with one confirmed activity", status)
	}
	if _, err := os.Stat(databasePath); err != nil {
		t.Fatalf("database file error = %v", err)
	}
}

func TestDaemonRejectsMissingDatabasePath(t *testing.T) {
	d, err := New(context.Background(), Config{PipeName: daemonPipeName()}, fixedClock{now: time.Now()})
	if d != nil {
		_ = d.Close()
		t.Fatal("New() returned daemon without database path")
	}
	if err == nil {
		t.Fatal("New() error = nil, want database path validation error")
	}
}

func TestDefaultConfigUsesPerUserLocalDatabaseAndPipeIdentity(t *testing.T) {
	config, err := DefaultConfig()
	if err != nil {
		t.Fatalf("DefaultConfig() error = %v", err)
	}
	if !filepath.IsAbs(config.DatabasePath) {
		t.Fatalf("database path = %q, want absolute per-user path", config.DatabasePath)
	}
	wantSuffix := filepath.Join("Walkout", "walkout.db")
	if !strings.HasSuffix(filepath.Clean(config.DatabasePath), wantSuffix) {
		t.Fatalf("database path = %q, want %q suffix", config.DatabasePath, wantSuffix)
	}
	identity, err := localipc.CurrentUserIdentity()
	if err != nil {
		t.Fatalf("CurrentUserIdentity() error = %v", err)
	}
	if config.PipeName != identity.PipeName {
		t.Fatalf("pipe name = %q, want %q", config.PipeName, identity.PipeName)
	}
}

func daemonPipeName() string {
	return fmt.Sprintf(
		`\\.\pipe\walkout-daemon-test-%d-%d`,
		os.Getpid(),
		daemonPipeSequence.Add(1),
	)
}

func serveDaemon(d *Daemon) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- d.Serve(context.Background())
	}()
	return done
}

func waitDaemon(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Serve() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("daemon did not stop")
	}
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return encoded
}
