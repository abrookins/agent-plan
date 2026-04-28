package main

import (
	"context"
	"os"

	"github.com/andrewbrookins/agent-plan"
)

var exitFunc = os.Exit

func run() int {
	return agentplan.Main(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}

func main() {
	exitFunc(run())
}
