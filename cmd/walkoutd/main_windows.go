//go:build windows

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/LexLuc/walkout/internal/daemon"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "walkoutd: %v\n", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	defaults, err := daemon.DefaultConfig()
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("walkoutd", flag.ContinueOnError)
	databasePath := flags.String("database", defaults.DatabasePath, "SQLite state database path")
	pipeName := flags.String("pipe", defaults.PipeName, "Windows named pipe path")
	if err := flags.Parse(arguments); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	instance, err := daemon.NewSystem(ctx, daemon.Config{
		DatabasePath: *databasePath,
		PipeName:     *pipeName,
	})
	if err != nil {
		return err
	}
	serveErr := instance.Serve(ctx)
	closeErr := instance.Close()
	return errors.Join(serveErr, closeErr)
}
