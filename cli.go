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
	case "init":
		return runInit(program, stdout, stderr, args[1:])
	case "create":
		return runCreate(program, stdout, stderr, args[1:])
	case "list":
		return runListPlans(stdout, stderr, args[1:])
	case "get":
		return runGet(program, stdout, stderr, args[1:])
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
	verbose := fs.Bool("verbose", false, "show additional details")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":     true,
		"--json":    false,
		"--verbose": false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s get [--dir PATH] [--json] [--verbose] <plan>\n", program)
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
	if *jsonOut {
		return writeJSON(stdout, plan, stderr)
	}
	fmt.Fprintln(stdout, formatPlan(*plan, *verbose))
	return 0
}

func runShow(program string, stdout, stderr io.Writer, args []string) int {
	fs := newCommandFlagSet("show", program, stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	verbose := fs.Bool("verbose", false, "show additional details")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":     true,
		"--json":    false,
		"--verbose": false,
	})); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s show [--dir PATH] [--json] [--verbose] <plan>\n", program)
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
	if *jsonOut {
		return writeJSON(stdout, plan, stderr)
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
	claimedBy := fs.String("claimed-by", "", "record the task as claimed by a specific agent id")
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
		"--claimed-by":         true,
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
		fmt.Fprintf(stderr, "usage: %s update-task [--dir PATH] [--status STATUS] [--name TEXT] [--description TEXT] [--claimed-by ID] [--clear-claimed-by] [--depends-on TASK] [--order N] [--clear-dependencies] [--json] <plan> <task>\n", program)
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
	var namePtr, descPtr, claimedByPtr *string
	if isFlagPassed(args, "--name") {
		value := *name
		namePtr = &value
	}
	if isFlagPassed(args, "--description") {
		value := *description
		descPtr = &value
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
		Usage:   "get [--dir PATH] [--json] [--verbose] <plan>",
		Details: []string{
			"Accepts a plan ID or exact plan name.",
			"Use show when you want the explicit whole-plan snapshot command name.",
		},
		Examples: []string{
			"get c1ebfc53 --json",
			`get "API Plan" --verbose`,
		},
	},
	"show": {
		Summary: "Show the current plan state and all tasks.",
		Usage:   "show [--dir PATH] [--json] [--verbose] <plan>",
		Details: []string{
			"Returns the same plan payload as get, but with a more explicit name for agent loops.",
			"Accepts a plan ID or exact plan name.",
		},
		Examples: []string{
			"show c1ebfc53 --json",
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
		Usage:   "update-task [--dir PATH] [--status STATUS] [--name TEXT] [--description TEXT] [--claimed-by ID] [--clear-claimed-by] [--depends-on TASK] [--order N] [--clear-dependencies] [--json] <plan> <task>",
		Details: []string{
			"Valid statuses: not_started, in_progress, blocked, complete, cancelled.",
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
}

var commandHelpOrder = []string{
	"init",
	"create",
	"list",
	"get",
	"show",
	"update",
	"delete",
	"add-task",
	"update-task",
	"delete-task",
	"ready",
	"next",
	"claim-next",
	"install",
	"mcp",
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
	line := fmt.Sprintf("- %s [%s] %s", task.ID, state, task.Name)
	if task.BlockedReason != "" {
		line += " reason=" + task.BlockedReason
	}
	if task.DescriptionPreview != "" {
		line += "\n  " + task.DescriptionPreview
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
	return nil, fmt.Errorf("plan not found: %s (expected plan ID or exact plan name)", cleaned)
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
	return nil, fmt.Errorf("%s not found: %s (expected task ID or exact task name)", label, cleaned)
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
