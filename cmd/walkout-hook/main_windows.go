//go:build windows

package main

import (
	"context"
	"os"

	"github.com/LexLuc/walkout/internal/hookcli"
)

func main() {
	os.Exit(hookcli.Run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
