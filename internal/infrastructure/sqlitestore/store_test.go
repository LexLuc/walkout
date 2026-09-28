package sqlitestore

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LexLuc/walkout/internal/application"
	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/protocol"
)

func TestStorePersistsResultAndLatestStateAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "walkout.db")
	store := mustOpenStore(t, path)
	want := testStoredResult("event-1", 7)
	applyCalls := 0

	got, err := store.ProcessOnce(context.Background(), "event-1", func() (application.StoredResult, error) {
		applyCalls++
		return want, nil
	})
	if err != nil {
		t.Fatalf("ProcessOnce() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ProcessOnce() = %#v, want %#v", got, want)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened := mustOpenStore(t, path)
	t.Cleanup(func() { _ = reopened.Close() })
	duplicate, err := reopened.ProcessOnce(context.Background(), "event-1", func() (application.StoredResult, error) {
		applyCalls++
		return testStoredResult("event-1", 99), nil
	})
	if err != nil {
		t.Fatalf("ProcessOnce() after reopen error = %v", err)
	}
	if applyCalls != 1 {
		t.Fatalf("apply calls = %d, want 1", applyCalls)
	}
	if !reflect.DeepEqual(duplicate, want) {
		t.Fatalf("duplicate result = %#v, want %#v", duplicate, want)
	}

	state, exists, err := reopened.LoadLatestState(context.Background())
	if err != nil {
		t.Fatalf("LoadLatestState() error = %v", err)
	}
	if !exists {
		t.Fatal("LoadLatestState() exists = false, want true")
	}
	if !reflect.DeepEqual(state, want.EngineState) {
		t.Fatalf("latest state = %#v, want %#v", state, want.EngineState)
	}
	restored, err := application.RestoreEngine(
		context.Background(),
		core.DefaultConfig(),
		fixedClock{now: time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)},
		reopened,
	)
	if err != nil {
		t.Fatalf("application.RestoreEngine() error = %v", err)
	}
	if got := restored.Snapshot().StateRevision; got != want.EngineState.StateRevision {
		t.Fatalf("restored state revision = %d, want %d", got, want.EngineState.StateRevision)
	}
}

func TestStoreRollsBackEventWhenLatestStateWriteFails(t *testing.T) {
	store := mustOpenStore(t, filepath.Join(t.TempDir(), "walkout.db"))
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	_, err := store.db.ExecContext(ctx, `
		CREATE TRIGGER fail_current_state_insert
		BEFORE INSERT ON current_state
		BEGIN
			SELECT RAISE(ABORT, 'simulated state write failure');
		END
	`)
	if err != nil {
		t.Fatalf("create failure trigger error = %v", err)
	}

	applyCalls := 0
	_, err = store.ProcessOnce(ctx, "event-rollback", func() (application.StoredResult, error) {
		applyCalls++
		return testStoredResult("event-rollback", 1), nil
	})
	if err == nil {
		t.Fatal("ProcessOnce() error = nil, want transaction failure")
	}
	assertRowCount(t, store, "processed_events", 0)
	assertRowCount(t, store, "current_state", 0)

	if _, err := store.db.ExecContext(ctx, `DROP TRIGGER fail_current_state_insert`); err != nil {
		t.Fatalf("drop failure trigger error = %v", err)
	}
	if _, err := store.ProcessOnce(ctx, "event-rollback", func() (application.StoredResult, error) {
		applyCalls++
		return testStoredResult("event-rollback", 1), nil
	}); err != nil {
		t.Fatalf("ProcessOnce() retry error = %v", err)
	}
	if applyCalls != 2 {
		t.Fatalf("apply calls = %d, want 2 after rolled-back retry", applyCalls)
	}
}

func TestStorePersistsCommandAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "walkout.db")
	store := mustOpenStore(t, path)
	want := testStoredCommandResult("command-1", 3)
	applyCalls := 0

	if _, err := store.ProcessCommandOnce(context.Background(), "command-1", func() (application.StoredCommandResult, error) {
		applyCalls++
		return want, nil
	}); err != nil {
		t.Fatalf("ProcessCommandOnce() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened := mustOpenStore(t, path)
	t.Cleanup(func() { _ = reopened.Close() })
	got, err := reopened.ProcessCommandOnce(context.Background(), "command-1", func() (application.StoredCommandResult, error) {
		applyCalls++
		return testStoredCommandResult("command-1", 99), nil
	})
	if err != nil {
		t.Fatalf("ProcessCommandOnce() after reopen error = %v", err)
	}
	if applyCalls != 1 {
		t.Fatalf("apply calls = %d, want 1", applyCalls)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("duplicate command result = %#v, want %#v", got, want)
	}
	state, exists, err := reopened.LoadLatestState(context.Background())
	if err != nil || !exists {
		t.Fatalf("LoadLatestState() = (%#v, %t, %v), want committed state", state, exists, err)
	}
	if !reflect.DeepEqual(state, want.EngineState) {
		t.Fatalf("latest state = %#v, want %#v", state, want.EngineState)
	}
}

func TestStoreRollsBackCommandWhenLatestStateWriteFails(t *testing.T) {
	store := mustOpenStore(t, filepath.Join(t.TempDir(), "walkout.db"))
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	_, err := store.db.ExecContext(ctx, `
		CREATE TRIGGER fail_command_state_insert
		BEFORE INSERT ON current_state
		BEGIN
			SELECT RAISE(ABORT, 'simulated command state write failure');
		END
	`)
	if err != nil {
		t.Fatalf("create failure trigger error = %v", err)
	}

	_, err = store.ProcessCommandOnce(ctx, "command-rollback", func() (application.StoredCommandResult, error) {
		return testStoredCommandResult("command-rollback", 1), nil
	})
	if err == nil {
		t.Fatal("ProcessCommandOnce() error = nil, want transaction failure")
	}
	assertRowCount(t, store, "processed_commands", 0)
	assertRowCount(t, store, "current_state", 0)
}

func TestOpenMigratesVersionOneDatabaseForCommands(t *testing.T) {
	path := filepath.Join(t.TempDir(), "walkout.db")
	store := mustOpenStore(t, path)
	if _, err := store.db.Exec(`DROP TABLE IF EXISTS processed_commands`); err != nil {
		t.Fatalf("drop processed_commands error = %v", err)
	}
	if _, err := store.db.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatalf("set version one error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	migrated := mustOpenStore(t, path)
	t.Cleanup(func() { _ = migrated.Close() })
	if _, err := migrated.ProcessCommandOnce(context.Background(), "after-migration", func() (application.StoredCommandResult, error) {
		return testStoredCommandResult("after-migration", 1), nil
	}); err != nil {
		t.Fatalf("ProcessCommandOnce() after v1 migration error = %v", err)
	}
	var version int
	if err := migrated.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read user_version error = %v", err)
	}
	if version != 2 {
		t.Fatalf("user_version = %d, want 2", version)
	}
}

func TestOpenRejectsUnknownSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "walkout.db")
	store := mustOpenStore(t, path)
	if _, err := store.db.Exec(`PRAGMA user_version = 99`); err != nil {
		t.Fatalf("set user_version error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	store, err := Open(path)
	if err == nil {
		_ = store.Close()
		t.Fatal("Open() error = nil, want unsupported schema error")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("Open() error = %v, want unsupported schema diagnostic", err)
	}
}

func TestStoreSerializesApplyCallbacks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "walkout.db")
	stores := []*Store{mustOpenStore(t, path), mustOpenStore(t, path)}
	for _, store := range stores {
		t.Cleanup(func() { _ = store.Close() })
	}

	var active int32
	var maximum int32
	var wait sync.WaitGroup
	errorsFound := make(chan error, 8)
	for index := range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			eventID := fmt.Sprintf("event-%d", index)
			store := stores[index%len(stores)]
			_, err := store.ProcessOnce(context.Background(), eventID, func() (application.StoredResult, error) {
				current := atomic.AddInt32(&active, 1)
				for observed := atomic.LoadInt32(&maximum); current > observed; observed = atomic.LoadInt32(&maximum) {
					if atomic.CompareAndSwapInt32(&maximum, observed, current) {
						break
					}
				}
				time.Sleep(5 * time.Millisecond)
				atomic.AddInt32(&active, -1)
				return testStoredResult(eventID, uint64(index+1)), nil
			})
			errorsFound <- err
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatalf("ProcessOnce() error = %v", err)
		}
	}
	if maximum != 1 {
		t.Fatalf("maximum concurrent apply callbacks = %d, want 1", maximum)
	}
	var integrity string
	if err := stores[0].db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatalf("PRAGMA integrity_check error = %v", err)
	}
	if integrity != "ok" {
		t.Fatalf("PRAGMA integrity_check = %q, want ok", integrity)
	}
}

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

func mustOpenStore(t *testing.T, path string) *Store {
	t.Helper()
	store, err := Open(path)
	if err != nil {
		absolutePath, _ := filepath.Abs(path)
		t.Fatalf("Open(%q, dsn %q) error = %v", path, dataSourceName(absolutePath), err)
	}
	return store
}

func testStoredResult(eventID string, revision uint64) application.StoredResult {
	return application.StoredResult{
		Decision: protocol.HealthDecision{
			SchemaVersion: protocol.SchemaVersion,
			DecisionID:    "decision-" + eventID,
			SourceEventID: eventID,
			StateRevision: revision,
			State:         protocol.StateWorking,
			Action:        protocol.ActionAllow,
			ReasonCode:    protocol.ReasonNotDue,
		},
		EngineState: core.EngineState{
			SchemaVersion: core.EngineStateSchemaVersion,
			State:         protocol.StateWorking,
			StateRevision: revision,
		},
	}
}

func testStoredCommandResult(commandID string, revision uint64) application.StoredCommandResult {
	return application.StoredCommandResult{
		Response: protocol.HealthControlResponse{
			SchemaVersion: protocol.SchemaVersion,
			CommandID:     commandID,
			CommandType:   protocol.CommandConfirmActivity,
			Status: protocol.HealthStatus{
				SchemaVersion: protocol.SchemaVersion,
				State:         protocol.StateWorking,
				StateRevision: revision,
			},
		},
		EngineState: core.EngineState{
			SchemaVersion: core.EngineStateSchemaVersion,
			State:         protocol.StateWorking,
			StateRevision: revision,
		},
	}
}

func assertRowCount(t *testing.T, store *Store, table string, want int) {
	t.Helper()
	var got int
	if err := store.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&got); err != nil {
		t.Fatalf("count %s error = %v", table, err)
	}
	if got != want {
		t.Fatalf("%s row count = %d, want %d", table, got, want)
	}
}
