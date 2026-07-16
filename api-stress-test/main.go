package main

import (
	"context"
	"os"

	"api-stress-test/cmd"
)

func main() {
	os.Exit(cmd.ExecuteContext(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}
