package agentplan

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"
)

func Main(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return MainNamed(ctx, "agent-plan", args, stdin, stdout, stderr)
}

func MainNamed(ctx context.Context, program string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runCLI(ctx, commandName(program), args, stdin, stdout, stderr)
}

func runCLI(ctx context.Context, program string, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		printUsage(stderr, program)
		return 1
	}

	switch args[0] {
	case "help", "-h", "--help":
		printUsage(stdout, program)
		return 0
	case "init":
		return runInit(program, stdout, stderr, args[1:])
	case "add":
		return runAdd(program, stdout, stderr, args[1:])
	case "list":
		return runList(stdout, stderr, args[1:])
	case "next":
		return runNext(stdout, stderr, args[1:])
	case "claim":
		return runClaim(program, stdout, stderr, args[1:])
	case "done":
		return runDone(program, stdout, stderr, args[1:])
	case "block":
		return runBlock(program, stdout, stderr, args[1:])
	case "plan":
		if len(args) > 1 && args[1] == "show" {
			return runPlanShow(program, stdout, stderr, args[2:])
		}
		fmt.Fprintf(stderr, "usage: %s plan show [--dir PATH] [--json]\n", program)
		return 1
	case "mcp":
		return runMCP(ctx, stdin, stdout, stderr, args[1:])
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		printUsage(stderr, program)
		return 1
	}
}

func runInit(_ string, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	store, err := Init(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]string{
			"root":     store.Root,
			"meta_dir": store.MetaDir,
		}, stderr)
	}
	fmt.Fprintf(stdout, "initialized %s\n", store.MetaDir)
	return 0
}

func runAdd(program string, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	description := fs.String("description", "", "task description")
	dependsOn := fs.String("depends-on", "", "comma-separated dependency ids")
	order := fs.Int("order", -1, "display order")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s add [--dir PATH] [--description TEXT] [--depends-on a,b] [--order N] <title>\n", program)
		return 1
	}
	store, err := OpenOrInit(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var orderValue *int
	if *order >= 0 {
		orderCopy := *order
		orderValue = &orderCopy
	}
	record, err := store.Add(fs.Arg(0), AddOptions{
		Description: *description,
		DependsOn:   splitCSV(*dependsOn),
		Order:       orderValue,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, record, stderr)
	}
	fmt.Fprintf(stdout, "added %s\n", record.ID)
	return 0
}

func runList(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	statusRaw := fs.String("status", "", "status filter")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	opts := ListOptions{}
	if value := strings.TrimSpace(*statusRaw); value != "" {
		status := Status(value)
		opts.Status = &status
	}
	tasks, err := store.List(opts)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, tasks, stderr)
	}
	for _, task := range tasks {
		fmt.Fprintln(stdout, formatTaskSummary(task))
	}
	return 0
}

func runNext(stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("next", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	task, err := store.Next()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, task, stderr)
	}
	if task == nil {
		fmt.Fprintln(stdout, "no ready task")
		return 0
	}
	fmt.Fprintln(stdout, formatTaskSummary(*task))
	return 0
}

func runClaim(program string, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("claim", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	by := fs.String("by", "", "actor id")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s claim [--dir PATH] [--by ACTOR] <id>\n", program)
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	record, err := store.Claim(fs.Arg(0), *by)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, record, stderr)
	}
	fmt.Fprintf(stdout, "claimed %s\n", record.ID)
	return 0
}

func runDone(program string, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("done", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	by := fs.String("by", "", "actor id")
	output := fs.String("output", "", "completion output")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s done [--dir PATH] [--by ACTOR] [--output TEXT] <id>\n", program)
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	record, err := store.Done(fs.Arg(0), *output, *by)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, record, stderr)
	}
	fmt.Fprintf(stdout, "completed %s\n", record.ID)
	return 0
}

func runBlock(program string, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("block", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	by := fs.String("by", "", "actor id")
	reason := fs.String("reason", "", "block reason")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s block [--dir PATH] [--by ACTOR] --reason TEXT <id>\n", program)
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	record, err := store.Block(fs.Arg(0), *reason, *by)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, record, stderr)
	}
	fmt.Fprintf(stdout, "blocked %s\n", record.ID)
	return 0
}

func runPlanShow(program string, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("plan show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	overview, err := store.Overview()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, overview, stderr)
	}
	fmt.Fprintln(stdout, "Plan")
	fmt.Fprintf(stdout, "  root: %s\n", store.Root)
	fmt.Fprintf(stdout, "  tasks: %d\n", len(overview.Manifest.Tasks))
	if len(overview.Counts) > 0 {
		fmt.Fprintf(stdout, "  counts: %v\n", overview.Counts)
	}
	if overview.Next != nil {
		fmt.Fprintf(stdout, "  next: %s\n", overview.Next.ID)
	}
	for _, task := range overview.Manifest.Tasks {
		fmt.Fprintln(stdout, formatTaskSummary(task))
	}
	return 0
}

func printUsage(w io.Writer, program string) {
	fmt.Fprintf(w, `Usage:
  %s init [--dir PATH] [--json]
  %s add [--dir PATH] [--description TEXT] [--depends-on a,b] [--order N] [--json] <title>
  %s list [--dir PATH] [--status STATUS] [--json]
  %s next [--dir PATH] [--json]
  %s claim [--dir PATH] [--by ACTOR] [--json] <id>
  %s done [--dir PATH] [--by ACTOR] [--output TEXT] [--json] <id>
  %s block [--dir PATH] [--by ACTOR] --reason TEXT [--json] <id>
  %s plan show [--dir PATH] [--json]
  %s mcp [--dir PATH]
`, program, program, program, program, program, program, program, program, program)
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Split(value, ",")
}

func writeJSON(w io.Writer, value any, stderr io.Writer) int {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func formatTaskSummary(task TaskSummary) string {
	state := string(task.Status)
	if task.ClaimedBy != "" {
		state += " claimed=" + task.ClaimedBy
	}
	line := fmt.Sprintf("- %s [%s] %s", task.ID, state, task.Title)
	if task.BlockedReason != "" {
		line += " reason=" + task.BlockedReason
	}
	if task.DescriptionPreview != "" {
		line += "\n  " + task.DescriptionPreview
	}
	return line
}

func commandName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "agent-plan"
	}
	return name
}
