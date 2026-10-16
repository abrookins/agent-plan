package agentplan

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
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
	cleanedArgs, pretty, err := extractPrettyFlag(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	args = cleanedArgs
	if pretty && !isFlagPassed(args, "--json") {
		fmt.Fprintln(stderr, "--pretty requires --json")
		return 1
	}
	if pretty {
		stdout = cliJSONWriter{Writer: stdout, pretty: true}
	}
	if len(args) > 1 && isHelpArg(args[1]) {
		if printCommandHelp(stdout, program, args[0]) {
			return 0
		}
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		printUsage(stderr, program)
		return 1
	}

	switch args[0] {
	case "help", "-h", "--help":
		if len(args) > 1 {
			if printCommandHelp(stdout, program, args[1]) {
				return 0
			}
			fmt.Fprintf(stderr, "unknown command: %s\n", args[1])
			return 1
		}
		printUsage(stdout, program)
		return 0
	case "version", "-v", "--version":
		return runVersion(program, stdout, stderr, args[1:])
	case "init":
		return runInit(program, stdout, stderr, args[1:])
	case "create":
		return runCreate(program, stdout, stderr, args[1:])
	case "list":
		return runListPlans(stdout, stderr, args[1:])
	case "get":
		return runGet(program, stdout, stderr, args[1:])
	case "get-task":
		return runGetTask(program, stdout, stderr, args[1:])
	case "show":
		return runShow(program, stdout, stderr, args[1:])
	case "update":
		return runUpdatePlan(program, stdout, stderr, args[1:])
	case "delete":
		return runDeletePlan(program, stdin, stdout, stderr, args[1:])
	case "add-task":
		return runAddTask(program, stdout, stderr, args[1:])
	case "update-task":
		return runUpdateTask(program, stdout, stderr, args[1:])
	case "delete-task":
		return runDeleteTask(program, stdout, stderr, args[1:])
	case "import":
		return runImport(program, stdin, stdout, stderr, args[1:])
	case "stats":
		return runStats(program, stdout, stderr, args[1:])
	case "audit":
		return runAudit(program, stdout, stderr, args[1:])
	case "lock":
		return runLock(program, stdout, stderr, args[1:])
	case "chain":
		return runChain(program, stdout, stderr, args[1:])
	case "ready":
		return runReady(program, stdout, stderr, args[1:])
	case "next":
		return runNext(program, stdout, stderr, args[1:])
	case "claim-next":
		return runClaimNext(program, stdout, stderr, args[1:])
	case "install":
		return runInstall(program, stdout, stderr, args[1:])
	case "mcp":
		if len(args) > 1 {
			fmt.Fprintf(stderr, "usage: %s mcp\n", program)
			return 1
		}
		return runMCP(ctx, stdin, stdout, stderr, args[1:])
	default:
		fmt.Fprintf(stderr, "unknown command: %s\n", args[0])
		printUsage(stderr, program)
		return 1
	}
}

func runVersion(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("version", program, stderr)
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--json": false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "usage: %s version [--json]\n", program)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]string{"version": Version}, stderr)
	}
	fmt.Fprintf(stdout, "%s %s\n", program, Version)
	return 0
}

func runInit(_ string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("init", "agent-plan", stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":  true,
		"--json": false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
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

func runCreate(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("create", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	description := fs.String("description", "", "plan description")
	specPath := fs.String("spec-path", "", "related spec path")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":         true,
		"--description": true,
		"--spec-path":   true,
		"--json":        false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s create [--dir PATH] [--description TEXT] [--spec-path PATH] [--json] <name>\n", program)
		return 1
	}
	store, err := OpenOrInit(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	plan, err := store.CreatePlan(fs.Arg(0), *description, *specPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, plan, stderr)
	}
	fmt.Fprintf(stdout, "Created plan: %s\nName: %s\n", plan.ID, plan.Name)
	return 0
}

func runListPlans(stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("list", "agent-plan", stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":  true,
		"--json": false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	plans, err := store.ListPlans()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, plans, stderr)
	}
	if len(plans) == 0 {
		fmt.Fprintln(stdout, "No plans found.")
		return 0
	}
	for _, plan := range plans {
		fmt.Fprintf(stdout, "%s: %s (%d/%d tasks complete)\n", plan.ID, plan.Name, plan.CompleteCount, plan.TaskCount)
	}
	return 0
}

func runGet(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("get", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	mermaidOut := fs.Bool("mermaid", false, "emit a Mermaid task graph")
	verbose := fs.Bool("verbose", false, "show additional details")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":     true,
		"--json":    false,
		"--mermaid": false,
		"--verbose": false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s get [--dir PATH] [--json | --mermaid] [--verbose] <plan>\n", program)
		return 1
	}
	if *jsonOut && *mermaidOut {
		fmt.Fprintln(stderr, "--json and --mermaid are mutually exclusive")
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		return writeCLIError(stdout, stderr, *jsonOut, err)
	}
	plan, err := resolvePlan(store, fs.Arg(0))
	if err != nil {
		return writeCLIError(stdout, stderr, *jsonOut, err)
	}
	if *jsonOut {
		return writeJSON(stdout, plan, stderr)
	}
	if *mermaidOut {
		fmt.Fprintln(stdout, formatPlanMermaid(*plan))
		return 0
	}
	fmt.Fprintln(stdout, formatPlan(*plan, *verbose))
	return 0
}

func runGetTask(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("get-task", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	parent := fs.String("parent", "", "filter by exact parent task")
	claimedBy := fs.String("claimed-by", "", "filter by claimer id")
	fieldsText := fs.String("fields", "", "comma-separated fields to return")
	limit := fs.Int("limit", defaultTaskQueryLimit, "maximum tasks to return")
	cursor := fs.String("cursor", "", "opaque pagination cursor")
	knownVersion := fs.Int("known-version", -1, "return unchanged when the plan version matches")
	jsonOut := fs.Bool("json", false, "emit json")
	pretty := fs.Bool("pretty", false, "pretty-print json")
	var statuses multiStringFlag
	fs.Var(&statuses, "status", "filter by status (repeatable or comma-separated)")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":           true,
		"--status":        true,
		"--parent":        true,
		"--claimed-by":    true,
		"--fields":        true,
		"--limit":         true,
		"--cursor":        true,
		"--known-version": true,
		"--json":          false,
		"--pretty":        false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() < 1 {
		fmt.Fprintf(stderr, "usage: %s get-task [options] <plan> [<task> ...]\n", program)
		return 1
	}
	if *pretty && !*jsonOut {
		fmt.Fprintln(stderr, "--pretty requires --json")
		return 1
	}
	if *knownVersion < -1 {
		return writeCLIError(
			stdout,
			stderr,
			*jsonOut,
			invalidArgumentError(errors.New("--known-version must be non-negative")),
		)
	}
	statusValues, err := parseTaskQueryStatuses(statuses.values)
	if err != nil {
		return writeCLIError(stdout, stderr, *jsonOut, invalidArgumentError(err))
	}
	fields := splitCommaValues([]string{*fieldsText})
	query := TaskQuery{
		TaskRefs:  append([]string(nil), fs.Args()[1:]...),
		Statuses:  statusValues,
		ParentRef: *parent,
		ClaimedBy: *claimedBy,
		Fields:    fields,
		Limit:     *limit,
		Cursor:    *cursor,
	}
	if *knownVersion >= 0 {
		query.KnownVersion = knownVersion
	}
	if len(query.TaskRefs) == 0 && len(query.Statuses) == 0 &&
		strings.TrimSpace(query.ParentRef) == "" && strings.TrimSpace(query.ClaimedBy) == "" {
		err := invalidArgumentError(
			errors.New("get-task requires at least one task reference or filter"),
		)
		return writeCLIError(stdout, stderr, *jsonOut, err)
	}
	store, err := Open(*dir)
	if err != nil {
		return writeCLIError(stdout, stderr, *jsonOut, err)
	}
	planID, err := resolvePlanID(store, fs.Arg(0))
	if err != nil {
		return writeCLIError(stdout, stderr, *jsonOut, err)
	}
	result, err := store.GetTasks(planID, query)
	if err != nil {
		return writeCLIError(stdout, stderr, *jsonOut, err)
	}
	if *jsonOut {
		return writeJSONWithIndent(stdout, result, stderr, *pretty)
	}
	if result.Unchanged {
		fmt.Fprintf(stdout, "Plan %s is unchanged at version %d.\n", result.PlanID, result.Version)
		return 0
	}
	if len(result.Tasks) == 0 {
		fmt.Fprintln(stdout, "No matching tasks found.")
		return 0
	}
	for _, task := range result.Tasks {
		fmt.Fprintln(stdout, formatTaskView(task, result.Fields))
	}
	if result.NextCursor != "" {
		fmt.Fprintf(stdout, "Next cursor: %s\n", result.NextCursor)
	}
	return 0
}

func runShow(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("show", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	mermaidOut := fs.Bool("mermaid", false, "emit a Mermaid task graph")
	verbose := fs.Bool("verbose", false, "show additional details")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":     true,
		"--json":    false,
		"--mermaid": false,
		"--verbose": false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s show [--dir PATH] [--json | --mermaid] [--verbose] <plan>\n", program)
		return 1
	}
	if *jsonOut && *mermaidOut {
		fmt.Fprintln(stderr, "--json and --mermaid are mutually exclusive")
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		return writeCLIError(stdout, stderr, *jsonOut, err)
	}
	plan, err := resolvePlan(store, fs.Arg(0))
	if err != nil {
		return writeCLIError(stdout, stderr, *jsonOut, err)
	}
	if *jsonOut {
		return writeJSON(stdout, plan, stderr)
	}
	if *mermaidOut {
		fmt.Fprintln(stdout, formatPlanMermaid(*plan))
		return 0
	}
	showVerbose := true
	if isFlagPassed(args, "--verbose") {
		showVerbose = *verbose
	}
	fmt.Fprintln(stdout, formatPlan(*plan, showVerbose))
	return 0
}

func runUpdatePlan(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("update", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	name := fs.String("name", "", "new plan name")
	description := fs.String("description", "", "new plan description")
	specPath := fs.String("spec-path", "", "related spec path")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":         true,
		"--name":        true,
		"--description": true,
		"--spec-path":   true,
		"--json":        false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s update [--dir PATH] [--name TEXT] [--description TEXT] [--spec-path PATH] [--json] <plan>\n", program)
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	plan, err := resolvePlan(store, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var namePtr, descPtr, specPtr *string
	if isFlagPassed(args, "--name") {
		nameCopy := *name
		namePtr = &nameCopy
	}
	if isFlagPassed(args, "--description") {
		descCopy := *description
		descPtr = &descCopy
	}
	if isFlagPassed(args, "--spec-path") {
		specCopy := *specPath
		specPtr = &specCopy
	}
	updated, err := store.UpdatePlan(plan.ID, PlanUpdateOptions{
		Name:        namePtr,
		Description: descPtr,
		SpecPath:    specPtr,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, updated, stderr)
	}
	fmt.Fprintf(stdout, "Updated plan: %s\n", updated.ID)
	fmt.Fprintln(stdout, formatPlan(*updated, true))
	return 0
}

func runDeletePlan(program string, stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("delete", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	yes := fs.Bool("yes", false, "skip confirmation")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":  true,
		"--yes":  false,
		"--json": false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s delete [--dir PATH] [--yes] [--json] <plan>\n", program)
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	plan, err := resolvePlan(store, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if !*yes && !nonInteractiveDelete() {
		if confirmed, err := confirmDelete(stdin, stdout, plan.Name); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		} else if !confirmed {
			if *jsonOut {
				return writeJSON(stdout, map[string]any{"deleted": false, "cancelled": true}, stderr)
			}
			fmt.Fprintln(stdout, "Cancelled.")
			return 0
		}
	}
	deleted, err := store.DeletePlan(plan.ID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"deleted": deleted, "id": plan.ID}, stderr)
	}
	if deleted {
		fmt.Fprintf(stdout, "Deleted plan: %s\n", plan.ID)
	} else {
		fmt.Fprintf(stderr, "Plan not found: %s\n", plan.ID)
		return 1
	}
	return 0
}

func runAddTask(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("add-task", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	description := fs.String("description", "", "task description")
	parent := fs.String("parent", "", "parent task name or id for subtasks")
	order := fs.Int("order", -1, "sibling order")
	jsonOut := fs.Bool("json", false, "emit json")
	var dependsOn multiStringFlag
	fs.Var(&dependsOn, "depends-on", "task id this task depends on (repeatable)")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":         true,
		"--description": true,
		"--parent":      true,
		"--order":       true,
		"--depends-on":  true,
		"--json":        false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 2 {
		fmt.Fprintf(stderr, "usage: %s add-task [--dir PATH] [--description TEXT] [--parent TASK] [--order N] [--depends-on TASK] [--json] <plan> <name>\n", program)
		return 1
	}
	store, err := OpenOrInit(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	plan, err := resolvePlan(store, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	parentID := ""
	if strings.TrimSpace(*parent) != "" {
		parentTask, err := resolveTask(plan, *parent, "parent task")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		parentID = parentTask.ID
	}
	dependencyIDs, err := resolveDependencyIDs(plan, dependsOn.values)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var orderPtr *int
	if *order >= 0 {
		value := *order
		orderPtr = &value
	}
	task, err := store.AddTask(plan.ID, fs.Arg(1), AddOptions{
		Description: *description,
		ParentID:    parentID,
		DependsOn:   dependencyIDs,
		Order:       orderPtr,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, task, stderr)
	}
	fmt.Fprintf(stdout, "Added task: %s\n", task.ID)
	fmt.Fprintln(stdout, formatTask(*task, 0))
	return 0
}

func runUpdateTask(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("update-task", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	status := fs.String("status", "", "new task status")
	name := fs.String("name", "", "new task name")
	description := fs.String("description", "", "new task description")
	parent := fs.String("parent", "", "new parent task name or id; pass empty to move to the root")
	claimedBy := fs.String("claimed-by", "", "record the task as claimed by a specific agent id")
	result := fs.String("result", "", "task decision, evidence, and handoff")
	clearClaimedBy := fs.Bool("clear-claimed-by", false, "remove any claimed-by value from the task")
	order := fs.Int("order", -1, "sibling order")
	clearDependencies := fs.Bool("clear-dependencies", false, "remove dependency links from the task")
	jsonOut := fs.Bool("json", false, "emit json")
	var dependsOn multiStringFlag
	fs.Var(&dependsOn, "depends-on", "task id this task depends on (repeatable)")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":                true,
		"--status":             true,
		"--name":               true,
		"--description":        true,
		"--parent":             true,
		"--claimed-by":         true,
		"--result":             true,
		"--clear-claimed-by":   false,
		"--depends-on":         true,
		"--order":              true,
		"--clear-dependencies": false,
		"--json":               false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 2 {
		fmt.Fprintf(stderr, "usage: %s update-task [--dir PATH] [--status STATUS] [--name TEXT] [--description TEXT] [--result TEXT] [--parent TASK] [--claimed-by ID] [--clear-claimed-by] [--depends-on TASK] [--order N] [--clear-dependencies] [--json] <plan> <task>\n", program)
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	plan, err := resolvePlan(store, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	task, err := resolveTask(plan, fs.Arg(1), "task")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var statusPtr *Status
	if isFlagPassed(args, "--status") {
		value := Status(strings.TrimSpace(*status))
		if !isValidStatus(value) {
			fmt.Fprintf(stderr, "invalid status: %s\n", value)
			return 1
		}
		statusPtr = &value
	}
	var namePtr, descPtr, resultPtr, claimedByPtr *string
	if isFlagPassed(args, "--name") {
		value := *name
		namePtr = &value
	}
	if isFlagPassed(args, "--description") {
		value := *description
		descPtr = &value
	}
	if isFlagPassed(args, "--result") {
		value := *result
		resultPtr = &value
	}
	var parentPtr *string
	if isFlagPassed(args, "--parent") {
		value := ""
		if strings.TrimSpace(*parent) != "" {
			parentTask, err := resolveTask(plan, *parent, "parent task")
			if err != nil {
				fmt.Fprintln(stderr, err)
				return 1
			}
			value = parentTask.ID
		}
		parentPtr = &value
	}
	if isFlagPassed(args, "--claimed-by") {
		value := *claimedBy
		claimedByPtr = &value
	}
	var deps []string
	if len(dependsOn.values) > 0 {
		deps, err = resolveDependencyIDs(plan, dependsOn.values)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	var orderPtr *int
	if *order >= 0 {
		value := *order
		orderPtr = &value
	}
	updated, err := store.UpdateTask(plan.ID, task.ID, TaskUpdateOptions{
		Status:            statusPtr,
		Name:              namePtr,
		Description:       descPtr,
		Result:            resultPtr,
		ParentID:          parentPtr,
		ClaimedBy:         claimedByPtr,
		ClearClaimedBy:    *clearClaimedBy,
		DependsOn:         deps,
		ClearDependencies: *clearDependencies,
		Order:             orderPtr,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, updated, stderr)
	}
	fmt.Fprintf(stdout, "Updated task: %s\n", updated.ID)
	fmt.Fprintln(stdout, formatTask(*updated, 0))
	return 0
}

func runDeleteTask(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("delete-task", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":  true,
		"--json": false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 2 {
		fmt.Fprintf(stderr, "usage: %s delete-task [--dir PATH] [--json] <plan> <task>\n", program)
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	plan, err := resolvePlan(store, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	task, err := resolveTask(plan, fs.Arg(1), "task")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	deleted, err := store.DeleteTask(plan.ID, task.ID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, map[string]any{"deleted": deleted, "id": task.ID}, stderr)
	}
	if deleted == 0 {
		fmt.Fprintf(stderr, "Task not found: %s\n", task.ID)
		return 1
	}
	fmt.Fprintf(stdout, "Deleted task: %s\n", task.ID)
	return 0
}

func runImport(program string, stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("import", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	filePath := fs.String("file", "-", "bulk import JSON file path, or - for stdin")
	dryRun := fs.Bool("dry-run", false, "validate without mutating the plan")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":     true,
		"--file":    true,
		"--dry-run": false,
		"--json":    false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s import [--dir PATH] [--file PATH] [--dry-run] [--json] <plan>\n", program)
		return 1
	}
	spec, err := readBulkImportSpec(*filePath, stdin)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	plan, err := resolvePlan(store, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	report, err := store.ImportTasks(plan.ID, spec, BulkImportOptions{DryRun: *dryRun})
	if err != nil {
		if *jsonOut && report != nil {
			_ = writeJSON(stdout, report, stderr)
		} else {
			fmt.Fprintln(stderr, err)
		}
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, report, stderr)
	}
	fmt.Fprintln(stdout, formatBulkImportReport(*report))
	return 0
}

func runStats(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("stats", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":  true,
		"--json": false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s stats [--dir PATH] [--json] <plan>\n", program)
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		return writeCLIError(stdout, stderr, *jsonOut, err)
	}
	planID, err := resolvePlanID(store, fs.Arg(0))
	if err != nil {
		return writeCLIError(stdout, stderr, *jsonOut, err)
	}
	stats, err := store.Stats(planID)
	if err != nil {
		return writeCLIError(stdout, stderr, *jsonOut, err)
	}
	if *jsonOut {
		return writeJSON(stdout, stats, stderr)
	}
	fmt.Fprintln(stdout, formatPlanStats(stats))
	return 0
}

func runAudit(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("audit", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":  true,
		"--json": false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s audit [--dir PATH] [--json] <plan>\n", program)
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		return writeCLIError(stdout, stderr, *jsonOut, err)
	}
	planID, err := resolvePlanID(store, fs.Arg(0))
	if err != nil {
		return writeCLIError(stdout, stderr, *jsonOut, err)
	}
	report, err := store.Audit(planID)
	if err != nil {
		return writeCLIError(stdout, stderr, *jsonOut, err)
	}
	if *jsonOut {
		return writeJSON(stdout, report, stderr)
	}
	fmt.Fprintln(stdout, formatAuditReport(report))
	return 0
}

func runLock(program string, stdout, stderr io.Writer, args []string) int {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "usage: %s lock <status|clear> [options] <plan>\n", program)
		return 1
	}
	switch args[0] {
	case "status":
		return runLockStatus(program, stdout, stderr, args[1:])
	case "clear":
		return runLockClear(program, stdout, stderr, args[1:])
	default:
		fmt.Fprintf(stderr, "unknown lock command: %s\n", args[0])
		return 1
	}
}

func runLockStatus(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("lock status", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":  true,
		"--json": false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s lock status [--dir PATH] [--json] <plan>\n", program)
		return 1
	}
	store, plan, err := resolveStoreAndPlan(*dir, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	status, err := store.PlanLockStatus(plan.ID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, status, stderr)
	}
	fmt.Fprintln(stdout, formatLockStatus(status))
	return 0
}

func runLockClear(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("lock clear", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	ifDead := fs.Bool("if-dead", false, "clear only when owner process is locally proven dead")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":     true,
		"--if-dead": false,
		"--json":    false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s lock clear [--dir PATH] --if-dead [--json] <plan>\n", program)
		return 1
	}
	if !*ifDead {
		fmt.Fprintln(stderr, "lock clear requires --if-dead")
		return 1
	}
	store, plan, err := resolveStoreAndPlan(*dir, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	result, err := store.ClearPlanLockIfDead(plan.ID)
	if err != nil {
		if *jsonOut {
			_ = writeJSON(stdout, result, stderr)
		} else {
			fmt.Fprintln(stderr, err)
		}
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, result, stderr)
	}
	if result.Cleared {
		fmt.Fprintf(stdout, "Cleared lock: %s\n", result.Status.Path)
	} else {
		fmt.Fprintf(stdout, "No lock to clear: %s\n", result.Status.Path)
	}
	return 0
}

func runChain(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("chain", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":  true,
		"--json": false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() < 3 {
		fmt.Fprintf(stderr, "usage: %s chain [--dir PATH] [--json] <plan> <task> <task> [task...]\n", program)
		return 1
	}
	store, plan, err := resolveStoreAndPlan(*dir, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	taskIDs := make([]string, 0, fs.NArg()-1)
	for _, ref := range fs.Args()[1:] {
		task, err := resolveTask(plan, ref, "task")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		taskIDs = append(taskIDs, task.ID)
	}
	updated, err := store.ChainTasks(plan.ID, taskIDs)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, updated, stderr)
	}
	fmt.Fprintf(stdout, "Chained %d tasks:\n", len(updated))
	for _, task := range updated {
		fmt.Fprintln(stdout, formatTask(task, 0))
	}
	return 0
}

func runReady(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("ready", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":  true,
		"--json": false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s ready [--dir PATH] [--json] <plan>\n", program)
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	plan, err := resolvePlan(store, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	ready := plan.ReadyTasks()
	if *jsonOut {
		payload := make([]map[string]any, 0, len(ready))
		for _, task := range ready {
			payload = append(payload, taskPayload(plan, task))
		}
		return writeJSON(stdout, payload, stderr)
	}
	if len(ready) == 0 {
		fmt.Fprintln(stdout, "No ready tasks found.")
		return 0
	}
	for _, task := range ready {
		fmt.Fprintln(stdout, formatTask(task, 0))
		claimedBy := task.ClaimedBy
		if claimedBy == "" {
			claimedBy = "(unclaimed)"
		}
		fmt.Fprintf(stdout, "    Claimed by: %s\n", claimedBy)
		if len(task.DependsOn) > 0 {
			fmt.Fprintf(stdout, "    Dependencies complete: %s\n", strings.Join(task.DependsOn, ", "))
		}
	}
	return 0
}

func runNext(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("next", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	by := fs.String("by", "", "prefer tasks already claimed by this agent id")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":  true,
		"--by":   true,
		"--json": false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s next [--dir PATH] [--by ID] [--json] <plan>\n", program)
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	plan, err := resolvePlan(store, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	task := plan.SelectNextTask(strings.TrimSpace(*by))
	if task == nil {
		fmt.Fprintln(stderr, "No ready tasks found.")
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, taskPayload(plan, *task), stderr)
	}
	fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", task.ID, task.Status, dashIfEmpty(task.ClaimedBy), task.Name)
	return 0
}

func runClaimNext(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("claim-next", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	by := fs.String("by", "", "agent id claiming the task")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":  true,
		"--by":   true,
		"--json": false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s claim-next [--dir PATH] [--by ID] [--json] <plan>\n", program)
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	plan, err := resolvePlan(store, fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	task, err := store.ClaimNext(plan.ID, *by)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if task == nil {
		fmt.Fprintln(stderr, "No ready tasks found.")
		return 1
	}
	plan, err = store.GetPlan(plan.ID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, taskPayload(plan, *task), stderr)
	}
	fmt.Fprintf(stdout, "Claimed task: %s\n", task.ID)
	fmt.Fprintln(stdout, formatTask(*task, 0))
	return 0
}

func runInstall(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("install", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	global := fs.Bool("global", false, "install into provider home instead of the current workspace")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":    true,
		"--global": false,
		"--json":   false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s install [--dir PATH] [--global] [--json] <AGENTS.md|CLAUDE.md|skill>\n", program)
		return 1
	}
	workspaceRoot := ""
	if !*global {
		root, err := normalizeRoot(*dir)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		workspaceRoot = root
	}
	path, err := InstallGuidance(fs.Arg(0), *global, workspaceRoot)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	payload := map[string]any{
		"target": fs.Arg(0),
		"scope":  ternary(*global, "global", "workspace"),
		"path":   path,
	}
	if *jsonOut {
		return writeJSON(stdout, payload, stderr)
	}
	fmt.Fprintf(stdout, "Installed %s guidance: %s\n", fs.Arg(0), path)
	return 0
}

type helpEntry struct {
	Summary  string
	Usage    string
	Details  []string
	Examples []string
}

var commandHelpEntries = map[string]helpEntry{
	"init": {
		Summary: "Initialize a workspace-local .agent-plan store.",
		Usage:   "init [--dir PATH] [--json]",
		Details: []string{
			"Creates the .agent-plan workspace metadata if it does not already exist.",
			"When --dir is omitted, the CLI falls back to AGENT_PLAN_DIR before using the current directory.",
		},
		Examples: []string{
			"init --dir /workspace --json",
		},
	},
	"create": {
		Summary: "Create a new plan.",
		Usage:   "create [--dir PATH] [--description TEXT] [--spec-path PATH] [--json] <name>",
		Details: []string{
			"Creates a plan record and an empty task manifest.",
			"Use a descriptive name and optional spec path for operator context.",
		},
		Examples: []string{
			`create "API Plan" --description "Track parity work" --json`,
		},
	},
	"list": {
		Summary: "List plans in the current workspace.",
		Usage:   "list [--dir PATH] [--json]",
		Details: []string{
			"Shows plan IDs, names, and completion counts.",
		},
	},
	"get": {
		Summary: "Load a plan and all of its tasks.",
		Usage:   "get [--dir PATH] [--json | --mermaid] [--verbose] <plan>",
		Details: []string{
			"Accepts a plan ID or exact plan name.",
			"Returns every task and can be large; prefer stats, ready, or get-task for routine work.",
			"Use show when you want the explicit whole-plan snapshot command name.",
			"Use --mermaid to emit a flowchart of hierarchy and dependency edges.",
		},
		Examples: []string{
			"get c1ebfc53 --json",
			`get "API Plan" --verbose`,
		},
	},
	"get-task": {
		Summary: "Retrieve selected tasks with bounded fields and pagination.",
		Usage:   "get-task [--dir PATH] [--status STATUS] [--parent TASK] [--claimed-by ID] [--fields FIELD,...] [--limit N] [--cursor CURSOR] [--known-version N] [--json] [--pretty] <plan> [<task> ...]",
		Details: []string{
			"Task references resolve by ID first and exact name second.",
			"At least one task reference or filter is required.",
			"Defaults to 25 tasks and bounded previews; full descriptions, outputs, paths, and timestamps require explicit fields.",
		},
		Examples: []string{
			"get-task c1ebfc53 001-design-store --json",
			"get-task c1ebfc53 --status in_progress --fields id,name,status,description_preview --json",
		},
	},
	"show": {
		Summary: "Show the current plan state and all tasks.",
		Usage:   "show [--dir PATH] [--json | --mermaid] [--verbose] <plan>",
		Details: []string{
			"Returns the same plan payload as get, but with a more explicit name for agent loops.",
			"Accepts a plan ID or exact plan name.",
			"Returns every task and can be large; prefer stats, ready, or get-task for routine work.",
			"Use --mermaid to emit a flowchart of hierarchy and dependency edges.",
		},
		Examples: []string{
			"show c1ebfc53 --json",
			"show c1ebfc53 --mermaid",
		},
	},
	"update": {
		Summary: "Update plan metadata.",
		Usage:   "update [--dir PATH] [--name TEXT] [--description TEXT] [--spec-path PATH] [--json] <plan>",
	},
	"delete": {
		Summary: "Delete a plan.",
		Usage:   "delete [--dir PATH] [--yes] [--json] <plan>",
		Details: []string{
			"Prompts for confirmation unless --yes is passed or AGENT_PLAN_NONINTERACTIVE=1 is set.",
			"Accepts a plan ID or exact plan name.",
		},
	},
	"add-task": {
		Summary: "Add a task to a plan.",
		Usage:   "add-task [--dir PATH] [--description TEXT] [--parent TASK] [--order N] [--depends-on TASK] [--json] <plan> <name>",
		Details: []string{
			"Dependency and parent references accept task IDs or exact task names, but IDs are safer for agents.",
		},
		Examples: []string{
			`add-task c1ebfc53 "Implement store" --depends-on 001-design-store --json`,
		},
	},
	"update-task": {
		Summary: "Update task status or metadata.",
		Usage:   "update-task [--dir PATH] [--status STATUS] [--name TEXT] [--description TEXT] [--result TEXT] [--parent TASK] [--claimed-by ID] [--clear-claimed-by] [--depends-on TASK] [--order N] [--clear-dependencies] [--json] <plan> <task>",
		Details: []string{
			"Valid statuses: not_started, in_progress, blocked, complete, cancelled.",
			"Use --result to preserve the task decision, evidence, and handoff after completion.",
			"Parent references accept task IDs or exact task names; pass an empty --parent value to move a task to the root.",
			"Setting --status complete archives the durable record into history/*.done.",
		},
		Examples: []string{
			"update-task c1ebfc53 001-design-store --status in_progress --json",
			"update-task c1ebfc53 001-design-store --status complete --json",
		},
	},
	"delete-task": {
		Summary: "Delete a task and any descendants.",
		Usage:   "delete-task [--dir PATH] [--json] <plan> <task>",
	},
	"import": {
		Summary: "Import or sync a structured task graph.",
		Usage:   "import [--dir PATH] [--file PATH] [--dry-run] [--json] <plan>",
		Details: []string{
			"Reads JSON with a tasks array and optional chains array.",
			"Validates the full graph before writing. --dry-run reports changes without mutating the plan.",
			"Task keys are persisted as import_key values so re-running the same input converges instead of duplicating tasks.",
		},
		Examples: []string{
			"import c1ebfc53 --file plan-tasks.json --dry-run --json",
			"import c1ebfc53 --file plan-tasks.json --json",
		},
	},
	"stats": {
		Summary: "Show compact plan counts.",
		Usage:   "stats [--dir PATH] [--json] <plan>",
		Details: []string{
			"Reports task, status, ready, claim, dependency, and history counts without returning every task.",
		},
	},
	"audit": {
		Summary: "Audit a plan for structural and coordination problems.",
		Usage:   "audit [--dir PATH] [--json] <plan>",
		Details: []string{
			"Reports duplicate names, bad parent/dependency references, cycles, stale locks, orphan files, and suspicious ready counts.",
		},
	},
	"lock": {
		Summary: "Inspect or safely clear a plan lock.",
		Usage:   "lock <status|clear> [options] <plan>",
		Details: []string{
			"Use `lock status <plan>` to inspect owner PID, host, age, and local liveness.",
			"Use `lock clear --if-dead <plan>` to remove a lock only when the local owner process is proven dead.",
		},
	},
	"chain": {
		Summary: "Add dependency edges between existing tasks.",
		Usage:   "chain [--dir PATH] [--json] <plan> <task> <task> [task...]",
		Details: []string{
			"Each task after the first is updated to depend on the previous task.",
			"Task references accept IDs or exact names. Existing dependencies are preserved and duplicates are removed.",
		},
	},
	"ready": {
		Summary: "List actionable tasks whose dependencies are complete.",
		Usage:   "ready [--dir PATH] [--json] <plan>",
		Details: []string{
			"Hierarchy alone does not block readiness; only depends_on relationships do.",
		},
	},
	"next": {
		Summary: "Select the next actionable task.",
		Usage:   "next [--dir PATH] [--by ID] [--json] <plan>",
		Details: []string{
			"With --by, prefers tasks already claimed by that agent before unclaimed ready work.",
			"Without --by, selection is unscoped and only returns unclaimed ready work.",
		},
	},
	"claim-next": {
		Summary: "Claim the next actionable task.",
		Usage:   "claim-next [--dir PATH] [--by ID] [--json] <plan>",
		Details: []string{
			"Claims through an exclusive .claim file and moves the task to in_progress.",
			"When --by is omitted, the CLI derives a local claimer id from AGENT_PLAN_ASSIGNEE, AGENT_NAME, or USER@HOST.",
		},
	},
	"install": {
		Summary: "Install managed guidance into AGENTS.md, CLAUDE.md, or a skill.",
		Usage:   "install [--dir PATH] [--global] [--json] <AGENTS.md|CLAUDE.md|skill>",
		Details: []string{
			"Intended for onboarding or setup, not for plan/task lifecycle operations.",
		},
	},
	"mcp": {
		Summary: "Run the MCP stdio server.",
		Usage:   "mcp [--dir PATH]",
		Details: []string{
			"Exposes the same plan workflow over MCP tools such as plan_show, plan_update_task, and plan_claim_next.",
		},
	},
	"version": {
		Summary: "Print the agent-plan version.",
		Usage:   "version [--json]",
		Details: []string{
			"Prints the same version advertised by the MCP server.",
		},
	},
}

var commandHelpOrder = []string{
	"init",
	"create",
	"list",
	"get",
	"get-task",
	"show",
	"update",
	"delete",
	"add-task",
	"update-task",
	"delete-task",
	"import",
	"stats",
	"audit",
	"lock",
	"chain",
	"ready",
	"next",
	"claim-next",
	"install",
	"mcp",
	"version",
}

func printUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage:\n  %s <command> [options]\n\nCommands:\n", program)
	for _, name := range commandHelpOrder {
		entry := commandHelpEntries[name]
		fmt.Fprintf(w, "  %-12s %s\n", name, entry.Summary)
	}
	fmt.Fprintf(w, "\nEnvironment:\n")
	fmt.Fprintf(w, "  AGENT_PLAN_DIR              Workspace root fallback when --dir is omitted.\n")
	fmt.Fprintf(w, "  AGENT_PLAN_NONINTERACTIVE   Set to 1 to skip delete confirmation prompts.\n")
	fmt.Fprintf(w, "\nExamples:\n")
	fmt.Fprintf(w, "  %s create \"API Plan\" --description \"Track parity work\" --json\n", program)
	fmt.Fprintf(w, "  %s show c1ebfc53 --json\n", program)
	fmt.Fprintf(w, "\nRun `%s help <command>` or `%s <command> --help` for command details.\n", program, program)
}

func printCommandHelp(w io.Writer, program, command string) bool {
	entry, ok := commandHelpEntries[command]
	if !ok {
		return false
	}
	fmt.Fprintf(w, "Usage:\n  %s %s\n\n", program, entry.Usage)
	fmt.Fprintf(w, "%s\n", entry.Summary)
	if len(entry.Details) > 0 {
		fmt.Fprintf(w, "\nDetails:\n")
		for _, detail := range entry.Details {
			fmt.Fprintf(w, "  - %s\n", detail)
		}
	}
	if len(entry.Examples) > 0 {
		fmt.Fprintf(w, "\nExamples:\n")
		for _, example := range entry.Examples {
			fmt.Fprintf(w, "  %s %s\n", program, example)
		}
	}
	return true
}

func newCommandFlagSet(name, program string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		_ = printCommandHelp(stderr, program, name)
	}
	return fs
}

func writeJSON(w io.Writer, value any, stderr io.Writer) int {
	return writeJSONWithIndent(w, value, stderr, jsonWriterPretty(w))
}

func writeJSONWithIndent(w io.Writer, value any, stderr io.Writer, pretty bool) int {
	encoder := json.NewEncoder(w)
	if pretty || jsonWriterPretty(w) {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(value); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func parseTaskQueryStatuses(values []string) ([]Status, error) {
	parts := splitCommaValues(values)
	statuses := make([]Status, 0, len(parts))
	for _, part := range parts {
		status := Status(part)
		if !isValidStatus(status) {
			return nil, fmt.Errorf("invalid status: %s", part)
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func splitCommaValues(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				result = append(result, part)
			}
		}
	}
	return result
}

func writeCLIError(stdout, stderr io.Writer, jsonOut bool, err error) int {
	if !jsonOut {
		fmt.Fprintln(stderr, err)
		return 1
	}
	writeJSON(stdout, ErrorEnvelope{OK: false, Error: errorObject(err)}, stderr)
	return 1
}

type cliJSONWriter struct {
	io.Writer
	pretty bool
}

func (w cliJSONWriter) JSONPretty() bool {
	return w.pretty
}

func jsonWriterPretty(w io.Writer) bool {
	type prettyWriter interface {
		JSONPretty() bool
	}
	if typed, ok := w.(prettyWriter); ok {
		return typed.JSONPretty()
	}
	return false
}

func extractPrettyFlag(args []string) ([]string, bool, error) {
	cleaned := make([]string, 0, len(args))
	pretty := false
	for _, arg := range args {
		if arg == "--pretty" {
			pretty = true
			continue
		}
		if strings.HasPrefix(arg, "--pretty=") {
			value, err := strconv.ParseBool(strings.TrimPrefix(arg, "--pretty="))
			if err != nil {
				return nil, false, fmt.Errorf("invalid value for --pretty: %w", err)
			}
			pretty = value
			continue
		}
		cleaned = append(cleaned, arg)
	}
	return cleaned, pretty, nil
}

func resolveStoreAndPlan(dir, planRef string) (*Store, *Plan, error) {
	store, err := Open(dir)
	if err != nil {
		return nil, nil, err
	}
	plan, err := resolvePlan(store, planRef)
	if err != nil {
		return nil, nil, err
	}
	return store, plan, nil
}

func readBulkImportSpec(path string, stdin io.Reader) (BulkImportSpec, error) {
	cleanedPath := strings.TrimSpace(path)
	var data []byte
	var err error
	if cleanedPath == "" || cleanedPath == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(cleanedPath)
	}
	if err != nil {
		return BulkImportSpec{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var spec BulkImportSpec
	if err := decoder.Decode(&spec); err != nil {
		return BulkImportSpec{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("bulk import JSON must contain a single object")
		}
		return BulkImportSpec{}, err
	}
	return spec, nil
}

func formatBulkImportReport(report BulkImportReport) string {
	mode := "Applied"
	if report.DryRun {
		mode = "Dry run"
	}
	lines := []string{
		fmt.Sprintf("%s import for plan %s: %d tasks (%d create, %d update, %d unchanged)", mode, report.PlanID, report.Total, report.Created, report.Updated, report.Unchanged),
	}
	if len(report.Errors) > 0 {
		lines = append(lines, "Errors:")
		for _, err := range report.Errors {
			lines = append(lines, "  - "+err)
		}
	}
	if len(report.Changes) > 0 {
		lines = append(lines, "Changes:")
		for _, change := range report.Changes {
			key := ""
			if change.Key != "" {
				key = " key=" + change.Key
			}
			lines = append(lines, fmt.Sprintf("  - %s %s%s %s", change.Action, change.ID, key, change.Name))
		}
	}
	return strings.Join(lines, "\n")
}

func formatPlanStats(stats PlanStats) string {
	lines := []string{
		fmt.Sprintf("Plan: %s (%s)", stats.Name, stats.PlanID),
		fmt.Sprintf("Tasks: %d total, %d ready, %d claimed", stats.Total, stats.ReadyCount, stats.ClaimedCount),
		fmt.Sprintf("Structure: %d root, %d parented, %d with dependencies", stats.RootCount, stats.ParentedCount, stats.DependencyCount),
		fmt.Sprintf("History: %d complete, next sequence %d", stats.HistoryCount, stats.NextSequence),
		"Statuses:",
	}
	for _, status := range []Status{StatusNotStarted, StatusInProgress, StatusBlocked, StatusComplete, StatusCancelled} {
		lines = append(lines, fmt.Sprintf("  %s: %d", status, stats.StatusCounts[status]))
	}
	return strings.Join(lines, "\n")
}

func formatAuditReport(report AuditReport) string {
	lines := []string{
		fmt.Sprintf("Audit: %s", report.PlanID),
		fmt.Sprintf("Findings: %d", len(report.Findings)),
	}
	if len(report.Findings) == 0 {
		lines = append(lines, "No findings.")
		return strings.Join(lines, "\n")
	}
	for _, finding := range report.Findings {
		target := finding.TaskID
		if target == "" {
			target = finding.Path
		}
		if target != "" {
			target = " " + target
		}
		lines = append(lines, fmt.Sprintf("- %s %s%s: %s", finding.Severity, finding.Code, target, finding.Message))
	}
	return strings.Join(lines, "\n")
}

func formatLockStatus(status LockStatus) string {
	if !status.Exists {
		return fmt.Sprintf("No lock: %s", status.Path)
	}
	alive := "unknown"
	if status.OwnerAlive != nil {
		if *status.OwnerAlive {
			alive = "true"
		} else {
			alive = "false"
		}
	}
	return strings.Join([]string{
		fmt.Sprintf("Lock: %s", status.Path),
		fmt.Sprintf("PID: %d", status.PID),
		fmt.Sprintf("Host: %s", dashIfEmpty(status.Host)),
		fmt.Sprintf("Created: %s", dashIfEmpty(status.CreatedAt)),
		fmt.Sprintf("Owner alive: %s", alive),
	}, "\n")
}

func formatTaskSummary(task TaskSummary) string {
	state := string(task.Status)
	if task.ClaimedBy != "" {
		state += " claimed=" + task.ClaimedBy
	}
	line := fmt.Sprintf("- %s [%s] %s", task.ID, state, task.Name)
	if task.BlockedReason != "" {
		line += " reason=" + task.BlockedReason
	}
	if task.DescriptionPreview != "" {
		line += "\n  " + task.DescriptionPreview
	}
	return line
}

func formatTaskView(task map[string]any, fields []string) string {
	id, _ := task["id"].(string)
	name, _ := task["name"].(string)
	status := fmt.Sprint(task["status"])
	if id == "" {
		id = "(task)"
	}
	if name == "" {
		name = "(unnamed)"
	}
	line := fmt.Sprintf("- %s [%s] %s", id, status, name)
	for _, field := range fields {
		if field == "id" || field == "name" || field == "status" {
			continue
		}
		value, ok := task[field]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case nil:
			line += fmt.Sprintf("\n    %s: -", field)
		case string:
			line += fmt.Sprintf("\n    %s: %s", field, typed)
		default:
			encoded, err := json.Marshal(typed)
			if err != nil {
				line += fmt.Sprintf("\n    %s: %v", field, typed)
			} else {
				line += fmt.Sprintf("\n    %s: %s", field, encoded)
			}
		}
	}
	return line
}

func formatTask(task TaskRecord, indent int) string {
	prefix := strings.Repeat("  ", indent)
	symbol := "[ ]"
	switch task.Status {
	case StatusInProgress:
		symbol = "[/]"
	case StatusComplete:
		symbol = "[x]"
	case StatusBlocked:
		symbol = "[!]"
	case StatusCancelled:
		symbol = "[-]"
	}
	line := fmt.Sprintf("%s%s %s: %s", prefix, symbol, task.ID, task.Name)
	if task.Description != "" {
		line += fmt.Sprintf("\n%s    %s", prefix, task.Description)
	}
	line += fmt.Sprintf("\n%s    Order: %d", prefix, task.Order)
	if len(task.DependsOn) > 0 {
		line += fmt.Sprintf("\n%s    Depends on: %s", prefix, strings.Join(task.DependsOn, ", "))
	}
	return line
}

func formatPlan(plan Plan, verbose bool) string {
	lines := []string{
		fmt.Sprintf("Plan: %s (%s)", plan.Name, plan.ID),
	}
	if plan.Description != "" {
		lines = append(lines, fmt.Sprintf("Description: %s", plan.Description))
	} else {
		lines = append(lines, "Description: ")
	}
	if plan.SpecPath != "" {
		lines = append(lines, fmt.Sprintf("Spec Path: %s", plan.SpecPath))
	}
	lines = append(lines, "", "Tasks:")

	var visit func(string, int)
	visit = func(parentID string, indent int) {
		for _, task := range plan.GetSubtasks(parentID) {
			lines = append(lines, formatTask(task, indent))
			visit(task.ID, indent+1)
		}
	}
	visit("", 0)
	if len(plan.Tasks) == 0 {
		lines = append(lines, "  (no tasks)")
	}
	if verbose {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("Version: %d", plan.Version))
		lines = append(lines, fmt.Sprintf("Updated: %s", plan.UpdatedAt.Format(timeFormat)))
	}
	return strings.Join(lines, "\n")
}

func formatPlanMermaid(plan Plan) string {
	lines := []string{
		fmt.Sprintf("%%%% Plan: %s (%s)", mermaidText(plan.Name), mermaidText(plan.ID)),
		"flowchart TD",
	}
	nodeIDs := make(map[string]string, len(plan.Tasks))
	for i, task := range plan.Tasks {
		nodeIDs[task.ID] = fmt.Sprintf("task%d", i)
	}
	for _, task := range plan.Tasks {
		label := mermaidText(task.ID) + "<br/>" + mermaidText(task.Name) + "<br/>" + mermaidText(string(task.Status))
		lines = append(lines, fmt.Sprintf("  %s[\"%s\"]:::%s", nodeIDs[task.ID], label, task.Status))
	}
	for _, task := range plan.Tasks {
		if parentNode, ok := nodeIDs[task.ParentID]; ok {
			lines = append(lines, fmt.Sprintf("  %s -. parent .-> %s", parentNode, nodeIDs[task.ID]))
		}
		for _, dependencyID := range task.DependsOn {
			if dependencyNode, ok := nodeIDs[dependencyID]; ok {
				lines = append(lines, fmt.Sprintf("  %s -->|blocks| %s", dependencyNode, nodeIDs[task.ID]))
			}
		}
	}
	lines = append(lines,
		"  classDef not_started fill:#f5f5f5,stroke:#616161,color:#212121",
		"  classDef in_progress fill:#e3f2fd,stroke:#1565c0,color:#0d47a1",
		"  classDef blocked fill:#ffebee,stroke:#c62828,color:#b71c1c",
		"  classDef complete fill:#e8f5e9,stroke:#2e7d32,color:#1b5e20",
		"  classDef cancelled fill:#eeeeee,stroke:#757575,color:#616161",
	)
	return strings.Join(lines, "\n")
}

func mermaidText(value string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"\"", "&quot;",
		"<", "&lt;",
		">", "&gt;",
		"\n", " ",
		"\r", " ",
	)
	return replacer.Replace(value)
}

const timeFormat = time.RFC3339Nano

func commandName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "agent-plan"
	}
	return name
}

type multiStringFlag struct {
	values []string
}

func (f *multiStringFlag) String() string { return strings.Join(f.values, ",") }
func (f *multiStringFlag) Set(value string) error {
	f.values = append(f.values, value)
	return nil
}

func resolvePlan(store *Store, planRef string) (*Plan, error) {
	cleaned := strings.TrimSpace(planRef)
	if cleaned == "" {
		return nil, fmt.Errorf("plan identifier is required")
	}
	if plan, err := store.GetPlan(cleaned); err == nil {
		return plan, nil
	} else if !errorHasCode(err, ErrorCodePlanNotFound) {
		return nil, err
	}
	summaries, err := store.ListPlans()
	if err != nil {
		return nil, err
	}
	exact := make([]PlanSummary, 0)
	for _, summary := range summaries {
		if summary.Name == cleaned {
			exact = append(exact, summary)
		}
	}
	if len(exact) == 1 {
		return store.GetPlan(exact[0].ID)
	}
	if len(exact) > 1 {
		ids := make([]string, 0, len(exact))
		for _, summary := range exact {
			ids = append(ids, summary.ID)
		}
		return nil, fmt.Errorf("multiple plans matched %q: %s", cleaned, strings.Join(ids, ", "))
	}
	return nil, &PlanError{
		ErrorCode: ErrorCodePlanNotFound,
		Message:   "expected plan ID or exact plan name",
		PlanID:    cleaned,
	}
}

func resolvePlanID(store *Store, planRef string) (string, error) {
	cleaned := strings.TrimSpace(planRef)
	if cleaned == "" {
		return "", fmt.Errorf("plan identifier is required")
	}
	record, err := store.loadPlanRecord(cleaned)
	if err == nil {
		return record.ID, nil
	}
	if !errorHasCode(err, ErrorCodePlanNotFound) {
		return "", err
	}
	entries, err := readDirFunc(store.plansDir())
	if err != nil {
		return "", err
	}
	matches := make([]string, 0, 1)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate, err := store.loadPlanRecord(entry.Name())
		if err != nil {
			return "", err
		}
		if candidate.Name == cleaned {
			matches = append(matches, candidate.ID)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple plans matched %q: %s", cleaned, strings.Join(matches, ", "))
	}
	return "", &PlanError{
		ErrorCode: ErrorCodePlanNotFound,
		Message:   "expected plan ID or exact plan name",
		PlanID:    cleaned,
	}
}

func resolveTask(plan *Plan, taskRef, label string) (*TaskRecord, error) {
	cleaned := strings.TrimSpace(taskRef)
	if cleaned == "" {
		return nil, fmt.Errorf("%s identifier is required", label)
	}
	if task := plan.GetTask(cleaned); task != nil {
		return task, nil
	}
	exact := make([]*TaskRecord, 0)
	for i := range plan.Tasks {
		if plan.Tasks[i].Name == cleaned {
			exact = append(exact, &plan.Tasks[i])
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		ids := make([]string, 0, len(exact))
		for _, task := range exact {
			ids = append(ids, task.ID)
		}
		return nil, fmt.Errorf("multiple tasks matched %q: %s", cleaned, strings.Join(ids, ", "))
	}
	return nil, &PlanError{
		ErrorCode: ErrorCodeTaskNotFound,
		Message:   label + " not found",
		TaskID:    cleaned,
	}
}

func resolveDependencyIDs(plan *Plan, refs []string) ([]string, error) {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		task, err := resolveTask(plan, ref, "dependency task")
		if err != nil {
			return nil, err
		}
		ids = append(ids, task.ID)
	}
	return plan.ValidateDependencyIDs(ids, "")
}

func taskPayload(plan *Plan, task TaskRecord) map[string]any {
	dependencyStatus := make([]map[string]any, 0, len(task.DependsOn))
	for _, dependencyID := range task.DependsOn {
		if dependency := plan.GetTask(dependencyID); dependency != nil {
			dependencyStatus = append(dependencyStatus, map[string]any{
				"id":     dependency.ID,
				"name":   dependency.Name,
				"status": dependency.Status,
			})
		}
	}
	return map[string]any{
		"id":                task.ID,
		"name":              task.Name,
		"status":            task.Status,
		"claimed_by":        emptyToNil(task.ClaimedBy),
		"depends_on":        append([]string(nil), task.DependsOn...),
		"dependency_status": dependencyStatus,
	}
}

func emptyToNil(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func isValidStatus(status Status) bool {
	switch status {
	case StatusNotStarted, StatusInProgress, StatusComplete, StatusBlocked, StatusCancelled:
		return true
	default:
		return false
	}
}

func confirmDelete(stdin io.Reader, stdout io.Writer, name string) (bool, error) {
	fmt.Fprintf(stdout, "Delete plan %q? [y/N]: ", name)
	reader := bufio.NewReader(stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func ternary[T any](condition bool, ifTrue, ifFalse T) T {
	if condition {
		return ifTrue
	}
	return ifFalse
}

func dashIfEmpty(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func nonInteractiveDelete() bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv("AGENT_PLAN_NONINTERACTIVE")))
	switch value {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func isHelpArg(value string) bool {
	switch strings.TrimSpace(value) {
	case "-h", "--help", "help":
		return true
	default:
		return false
	}
}

func isFlagPassed(args []string, name string) bool {
	for i, arg := range args {
		if arg == name {
			return true
		}
		if strings.HasPrefix(arg, name+"=") {
			return true
		}
		if i == 0 {
			continue
		}
	}
	return false
}

func reorderFlags(args []string, takesValue map[string]bool) []string {
	if len(args) == 0 {
		return args
	}
	flags := make([]string, 0, len(args))
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positionals = append(positionals, arg)
			continue
		}
		flags = append(flags, arg)
		if strings.Contains(arg, "=") {
			continue
		}
		if !takesValue[arg] {
			continue
		}
		if i+1 < len(args) {
			flags = append(flags, args[i+1])
			i++
		}
	}
	return append(flags, positionals...)
}
