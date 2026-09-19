package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/bettertomorrow-dev/tlgme/internal/app"
)

func main() {
	os.Exit(run())
}

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return app.Run(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}
