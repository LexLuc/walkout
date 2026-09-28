//go:build windows

package main

import (
	"context"
	"os"

	"github.com/LexLuc/walkout/internal/ctlcli"
)

func main() {
	os.Exit(ctlcli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
