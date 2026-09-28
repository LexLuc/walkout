//go:build windows

package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/LexLuc/walkout/internal/application"
	"github.com/LexLuc/walkout/internal/core"
	"github.com/LexLuc/walkout/internal/infrastructure/sqlitestore"
	"github.com/LexLuc/walkout/internal/transport/localipc"
	"github.com/LexLuc/walkout/internal/transport/ndjson"
)

type Config struct {
	DatabasePath string
	PipeName     string
}

type Daemon struct {
	server *localipc.Server
	store  *sqlitestore.Store

	closeOnce sync.Once
	closeErr  error
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
}

func DefaultConfig() (Config, error) {
	cacheDirectory, err := os.UserCacheDir()
	if err != nil {
		return Config{}, fmt.Errorf("resolve per-user data directory: %w", err)
	}
	identity, err := localipc.CurrentUserIdentity()
	if err != nil {
		return Config{}, err
	}
	return Config{
		DatabasePath: filepath.Join(cacheDirectory, "Walkout", "walkout.db"),
		PipeName:     identity.PipeName,
	}, nil
}

func New(ctx context.Context, config Config, clock core.Clock) (*Daemon, error) {
	if config.DatabasePath == "" {
		return nil, errors.New("database path is required")
	}
	if clock == nil {
		return nil, errors.New("clock is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(config.DatabasePath), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	store, err := sqlitestore.Open(config.DatabasePath)
	if err != nil {
		return nil, err
	}
	service, err := application.NewService(
		ctx,
		core.DefaultConfig(),
		clock,
		core.NewPolicy(),
		store,
	)
	if err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("restore application service: %w", err)
	}
	handler, err := ndjson.NewHandler(service, 0)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	server, err := localipc.NewServer(localipc.Config{PipeName: config.PipeName}, handler)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	return &Daemon{server: server, store: store}, nil
}

func NewSystem(ctx context.Context, config Config) (*Daemon, error) {
	return New(ctx, config, systemClock{})
}

func (d *Daemon) Serve(ctx context.Context) error {
	return d.server.Serve(ctx)
}

func (d *Daemon) Close() error {
	d.closeOnce.Do(func() {
		serverErr := d.server.Close()
		storeErr := d.store.Close()
		d.closeErr = errors.Join(serverErr, storeErr)
	})
	return d.closeErr
}
