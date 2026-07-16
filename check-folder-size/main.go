package main

import (
	"context"
	"os"

	"check-folder-size/cmd"
)

func main() {
	os.Exit(cmd.ExecuteContext(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
