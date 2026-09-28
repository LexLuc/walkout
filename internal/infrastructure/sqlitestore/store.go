package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sync"
	"time"

	"github.com/LexLuc/walkout/internal/application"
	"github.com/LexLuc/walkout/internal/core"

	_ "github.com/ncruces/go-sqlite3/driver"
)

const schemaVersion = 2

type Store struct {
	mu     sync.Mutex
	db     *sql.DB
	closed bool
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is required")
	}
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve database path: %w", err)
	}

	db, err := sql.Open("sqlite3", dataSourceName(absolutePath))
	if err != nil {
		return nil, fmt.Errorf("open SQLite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := initialize(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) ProcessOnce(
	ctx context.Context,
	eventID string,
	apply func() (application.StoredResult, error),
) (application.StoredResult, error) {
	if eventID == "" {
		return application.StoredResult{}, errors.New("event ID is required")
	}
	if apply == nil {
		return application.StoredResult{}, errors.New("apply function is required")
	}
	if err := ctx.Err(); err != nil {
		return application.StoredResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return application.StoredResult{}, errors.New("SQLite result store is closed")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return application.StoredResult{}, fmt.Errorf("begin result transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, exists, err := loadResult(ctx, tx, eventID)
	if err != nil {
		return application.StoredResult{}, err
	}
	if exists {
		if err := tx.Commit(); err != nil {
			return application.StoredResult{}, fmt.Errorf("finish duplicate lookup: %w", err)
		}
		return result, nil
	}

	result, err = apply()
	if err != nil {
		return application.StoredResult{}, err
	}
	if result.Decision.SourceEventID != eventID {
		return application.StoredResult{}, errors.New("decision source event ID does not match event ID")
	}
	decisionJSON, err := json.Marshal(result.Decision)
	if err != nil {
		return application.StoredResult{}, fmt.Errorf("encode health decision: %w", err)
	}
	stateJSON, err := json.Marshal(result.EngineState)
	if err != nil {
		return application.StoredResult{}, fmt.Errorf("encode engine state: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO processed_events(event_id, decision_json, engine_state_json) VALUES (?, ?, ?)`,
		eventID,
		decisionJSON,
		stateJSON,
	); err != nil {
		return application.StoredResult{}, fmt.Errorf("store processed event: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO current_state(singleton, engine_state_json) VALUES (1, ?)
		 ON CONFLICT(singleton) DO UPDATE SET engine_state_json = excluded.engine_state_json`,
		stateJSON,
	); err != nil {
		return application.StoredResult{}, fmt.Errorf("store latest engine state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return application.StoredResult{}, fmt.Errorf("commit processed event: %w", err)
	}
	return result, nil
}

func (s *Store) ProcessCommandOnce(
	ctx context.Context,
	commandID string,
	apply func() (application.StoredCommandResult, error),
) (application.StoredCommandResult, error) {
	if commandID == "" {
		return application.StoredCommandResult{}, errors.New("command ID is required")
	}
	if apply == nil {
		return application.StoredCommandResult{}, errors.New("apply function is required")
	}
	if err := ctx.Err(); err != nil {
		return application.StoredCommandResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return application.StoredCommandResult{}, errors.New("SQLite result store is closed")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return application.StoredCommandResult{}, fmt.Errorf("begin command transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, exists, err := loadCommandResult(ctx, tx, commandID)
	if err != nil {
		return application.StoredCommandResult{}, err
	}
	if exists {
		if err := tx.Commit(); err != nil {
			return application.StoredCommandResult{}, fmt.Errorf("finish duplicate command lookup: %w", err)
		}
		return result, nil
	}

	result, err = apply()
	if err != nil {
		return application.StoredCommandResult{}, err
	}
	if result.Response.CommandID != commandID {
		return application.StoredCommandResult{}, errors.New("response command ID does not match command ID")
	}
	responseJSON, err := json.Marshal(result.Response)
	if err != nil {
		return application.StoredCommandResult{}, fmt.Errorf("encode control response: %w", err)
	}
	stateJSON, err := json.Marshal(result.EngineState)
	if err != nil {
		return application.StoredCommandResult{}, fmt.Errorf("encode command engine state: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO processed_commands(command_id, response_json, engine_state_json) VALUES (?, ?, ?)`,
		commandID,
		responseJSON,
		stateJSON,
	); err != nil {
		return application.StoredCommandResult{}, fmt.Errorf("store processed command: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO current_state(singleton, engine_state_json) VALUES (1, ?)
		 ON CONFLICT(singleton) DO UPDATE SET engine_state_json = excluded.engine_state_json`,
		stateJSON,
	); err != nil {
		return application.StoredCommandResult{}, fmt.Errorf("store command engine state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return application.StoredCommandResult{}, fmt.Errorf("commit processed command: %w", err)
	}
	return result, nil
}

func (s *Store) LoadLatestState(ctx context.Context) (core.EngineState, bool, error) {
	if err := ctx.Err(); err != nil {
		return core.EngineState{}, false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return core.EngineState{}, false, errors.New("SQLite result store is closed")
	}

	var encoded []byte
	err := s.db.QueryRowContext(
		ctx,
		`SELECT engine_state_json FROM current_state WHERE singleton = 1`,
	).Scan(&encoded)
	if errors.Is(err, sql.ErrNoRows) {
		return core.EngineState{}, false, nil
	}
	if err != nil {
		return core.EngineState{}, false, fmt.Errorf("load latest engine state: %w", err)
	}
	var state core.EngineState
	if err := json.Unmarshal(encoded, &state); err != nil {
		return core.EngineState{}, false, fmt.Errorf("decode latest engine state: %w", err)
	}
	return state, true, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.db.Close()
}

func initialize(ctx context.Context, db *sql.DB) error {
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("connect SQLite: %w", err)
	}
	for _, statement := range []string{
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA journal_mode = WAL`,
		`PRAGMA foreign_keys = ON`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure SQLite: %w", err)
		}
	}
	return migrate(ctx, db)
}

func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SQLite migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var version int
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read SQLite schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("unsupported SQLite schema version %d", version)
	}
	if version < 0 {
		return fmt.Errorf("invalid SQLite schema version %d", version)
	}

	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS processed_events (
			event_id TEXT PRIMARY KEY,
			decision_json BLOB NOT NULL CHECK(length(decision_json) > 0),
			engine_state_json BLOB NOT NULL CHECK(length(engine_state_json) > 0),
			committed_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		) STRICT`,
		`CREATE TABLE IF NOT EXISTS current_state (
			singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
			engine_state_json BLOB NOT NULL CHECK(length(engine_state_json) > 0)
		) STRICT`,
		`CREATE TABLE IF NOT EXISTS processed_commands (
			command_id TEXT PRIMARY KEY,
			response_json BLOB NOT NULL CHECK(length(response_json) > 0),
			engine_state_json BLOB NOT NULL CHECK(length(engine_state_json) > 0),
			committed_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		) STRICT`,
		fmt.Sprintf(`PRAGMA user_version = %d`, schemaVersion),
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply SQLite migration: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit SQLite migration: %w", err)
	}
	return nil
}

func loadResult(ctx context.Context, tx *sql.Tx, eventID string) (application.StoredResult, bool, error) {
	var decisionJSON []byte
	var stateJSON []byte
	err := tx.QueryRowContext(
		ctx,
		`SELECT decision_json, engine_state_json FROM processed_events WHERE event_id = ?`,
		eventID,
	).Scan(&decisionJSON, &stateJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return application.StoredResult{}, false, nil
	}
	if err != nil {
		return application.StoredResult{}, false, fmt.Errorf("load processed event: %w", err)
	}

	var result application.StoredResult
	if err := json.Unmarshal(decisionJSON, &result.Decision); err != nil {
		return application.StoredResult{}, false, fmt.Errorf("decode health decision: %w", err)
	}
	if err := json.Unmarshal(stateJSON, &result.EngineState); err != nil {
		return application.StoredResult{}, false, fmt.Errorf("decode event engine state: %w", err)
	}
	return result, true, nil
}

func loadCommandResult(
	ctx context.Context,
	tx *sql.Tx,
	commandID string,
) (application.StoredCommandResult, bool, error) {
	var responseJSON []byte
	var stateJSON []byte
	err := tx.QueryRowContext(
		ctx,
		`SELECT response_json, engine_state_json FROM processed_commands WHERE command_id = ?`,
		commandID,
	).Scan(&responseJSON, &stateJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return application.StoredCommandResult{}, false, nil
	}
	if err != nil {
		return application.StoredCommandResult{}, false, fmt.Errorf("load processed command: %w", err)
	}

	var result application.StoredCommandResult
	if err := json.Unmarshal(responseJSON, &result.Response); err != nil {
		return application.StoredCommandResult{}, false, fmt.Errorf("decode control response: %w", err)
	}
	if err := json.Unmarshal(stateJSON, &result.EngineState); err != nil {
		return application.StoredCommandResult{}, false, fmt.Errorf("decode command engine state: %w", err)
	}
	return result, true, nil
}

func dataSourceName(path string) string {
	dsn := &url.URL{Scheme: "file", OmitHost: true, Path: path}
	query := dsn.Query()
	query.Set("_txlock", "immediate")
	dsn.RawQuery = query.Encode()
	return dsn.String()
}

var _ application.Store = (*Store)(nil)
