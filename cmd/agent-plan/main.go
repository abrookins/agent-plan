package main

import (
	"context"
	"os"

	"github.com/andrewbrookins/agent-plan"
)

func main() {
	os.Exit(agentplan.Main(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
