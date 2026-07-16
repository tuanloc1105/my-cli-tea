package main

import (
	"context"
	"os"
	"os/signal"

	"find-everything/cmd"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(cmd.ExecuteContext(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
