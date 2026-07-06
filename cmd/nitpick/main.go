// Command nitpick is a local-first CLI/TUI for working with pull-request review.
package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/sjrbie/nitpick/internal/cli"
)

func main() {
	// Cancel the context on Ctrl-C so in-flight HTTP requests unwind cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
