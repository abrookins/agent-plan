package agentplan

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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

	switch args[0] {
	case "help", "-h", "--help":
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
	case "bind-session":
		return runBindSession(program, stdout, stderr, args[1:])
	case "bind-current":
		return runBindCurrent(program, stdout, stderr, args[1:])
	case "install":
		return runInstall(program, stdout, stderr, args[1:])
	case "list-tasks":
		return runListTasks(program, stdout, stderr, args[1:])
	case "add":
		return runAdd(program, stdout, stderr, args[1:])
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
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":  true,
		"--json": false,
	})); err != nil {
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
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	fs.SetOutput(stderr)
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
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":  true,
		"--json": false,
	})); err != nil {
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
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	verbose := fs.Bool("verbose", false, "show additional details")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":     true,
		"--json":    false,
		"--verbose": false,
	})); err != nil {
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

func runUpdatePlan(program string, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	name := fs.String("name", "", "new plan name")
	description := fs.String("description", "", "new plan description")
	specPath := fs.String("spec-path", "", "related spec path")
	clearSessions := fs.Bool("clear-sessions", false, "remove all assigned session ids")
	jsonOut := fs.Bool("json", false, "emit json")
	var sessionIDs multiStringFlag
	fs.Var(&sessionIDs, "session-id", "session id allowed to use this plan (repeatable)")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":            true,
		"--name":           true,
		"--description":    true,
		"--session-id":     true,
		"--spec-path":      true,
		"--clear-sessions": false,
		"--json":           false,
	})); err != nil {
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s update [--dir PATH] [--name TEXT] [--description TEXT] [--session-id ID] [--spec-path PATH] [--clear-sessions] [--json] <plan>\n", program)
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
		Name:          namePtr,
		Description:   descPtr,
		SpecPath:      specPtr,
		SessionIDs:    sessionIDs.values,
		ClearSessions: *clearSessions,
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
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	yes := fs.Bool("yes", false, "skip confirmation")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":  true,
		"--yes":  false,
		"--json": false,
	})); err != nil {
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
	if !*yes {
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
	fs := flag.NewFlagSet("add-task", flag.ContinueOnError)
	fs.SetOutput(stderr)
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
	fs := flag.NewFlagSet("update-task", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	status := fs.String("status", "", "new task status")
	name := fs.String("name", "", "new task name")
	description := fs.String("description", "", "new task description")
	assignedSession := fs.String("assigned-session", "", "assign task to a specific session id")
	clearAssigned := fs.Bool("clear-assigned-session", false, "remove any assigned session id from the task")
	order := fs.Int("order", -1, "sibling order")
	clearDependencies := fs.Bool("clear-dependencies", false, "remove dependency links from the task")
	jsonOut := fs.Bool("json", false, "emit json")
	var dependsOn multiStringFlag
	fs.Var(&dependsOn, "depends-on", "task id this task depends on (repeatable)")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":                    true,
		"--status":                 true,
		"--name":                   true,
		"--description":            true,
		"--assigned-session":       true,
		"--clear-assigned-session": false,
		"--depends-on":             true,
		"--order":                  true,
		"--clear-dependencies":     false,
		"--json":                   false,
	})); err != nil {
		return 1
	}
	if fs.NArg() != 2 {
		fmt.Fprintf(stderr, "usage: %s update-task [--dir PATH] [--status STATUS] [--name TEXT] [--description TEXT] [--assigned-session ID] [--clear-assigned-session] [--depends-on TASK] [--order N] [--clear-dependencies] [--json] <plan> <task>\n", program)
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
	var namePtr, descPtr, assignedPtr *string
	if isFlagPassed(args, "--name") {
		value := *name
		namePtr = &value
	}
	if isFlagPassed(args, "--description") {
		value := *description
		descPtr = &value
	}
	if isFlagPassed(args, "--assigned-session") {
		value := *assignedSession
		assignedPtr = &value
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
		Status:               statusPtr,
		Name:                 namePtr,
		Description:          descPtr,
		AssignedSessionID:    assignedPtr,
		ClearAssignedSession: *clearAssigned,
		DependsOn:            deps,
		ClearDependencies:    *clearDependencies,
		Order:                orderPtr,
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
	fs := flag.NewFlagSet("delete-task", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":  true,
		"--json": false,
	})); err != nil {
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
	fs := flag.NewFlagSet("ready", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":  true,
		"--json": false,
	})); err != nil {
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	planID, err := resolvePlanIDForTaskCommand(store, fs.Args())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	plan, err := store.GetPlan(planID)
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
		assigned := task.AssignedSessionID
		if assigned == "" {
			assigned = "(unassigned)"
		}
		fmt.Fprintf(stdout, "    Assigned session: %s\n", assigned)
		if len(task.DependsOn) > 0 {
			fmt.Fprintf(stdout, "    Dependencies complete: %s\n", strings.Join(task.DependsOn, ", "))
		}
	}
	return 0
}

func runNext(program string, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("next", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	sessionID := fs.String("session", "", "prefer tasks for this session")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":     true,
		"--session": true,
		"--json":    false,
	})); err != nil {
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	planID, err := resolvePlanIDForTaskCommand(store, fs.Args())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	plan, err := store.GetPlan(planID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	task := plan.SelectNextTask(strings.TrimSpace(*sessionID))
	if task == nil {
		fmt.Fprintln(stderr, "No ready tasks found.")
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, taskPayload(plan, *task), stderr)
	}
	fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", task.ID, task.Status, dashIfEmpty(task.AssignedSessionID), task.Name)
	return 0
}

func runClaimNext(program string, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("claim-next", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	sessionID := fs.String("session", "", "session claiming the task")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":     true,
		"--session": true,
		"--json":    false,
	})); err != nil {
		return 1
	}
	if strings.TrimSpace(*sessionID) == "" {
		fmt.Fprintf(stderr, "usage: %s claim-next [--dir PATH] --session ID [--json] <plan>\n", program)
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	planID, err := resolvePlanIDForTaskCommand(store, fs.Args())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	task, err := store.ClaimNext(planID, *sessionID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if task == nil {
		fmt.Fprintln(stderr, "No ready tasks found.")
		return 1
	}
	plan, err := store.GetPlan(planID)
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

func runBindSession(program string, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("bind-session", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	sessionID := fs.String("session", "", "session id to bind")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":     true,
		"--session": true,
		"--json":    false,
	})); err != nil {
		return 1
	}
	if fs.NArg() != 1 || strings.TrimSpace(*sessionID) == "" {
		fmt.Fprintf(stderr, "usage: %s bind-session [--dir PATH] --session ID [--json] <plan>\n", program)
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
	binding, err := store.BindSession(plan.ID, *sessionID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, binding, stderr)
	}
	fmt.Fprintf(stdout, "Bound session %s to plan %s (%s)\n", binding.SessionID, binding.PlanName, binding.PlanID)
	if binding.CurrentTaskID != "" {
		fmt.Fprintf(stdout, "Current task: %s %s\n", binding.CurrentTaskID, binding.CurrentTaskName)
	}
	return 0
}

func runBindCurrent(program string, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("bind-current", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":  true,
		"--json": false,
	})); err != nil {
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s bind-current [--dir PATH] [--json] <plan>\n", program)
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
	binding, err := store.BindCurrent(plan.ID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *jsonOut {
		return writeJSON(stdout, binding, stderr)
	}
	fmt.Fprintf(stdout, "Bound current session %s to plan %s (%s)\n", binding.SessionID, binding.PlanName, binding.PlanID)
	if binding.CurrentTaskID != "" {
		fmt.Fprintf(stdout, "Current task: %s %s\n", binding.CurrentTaskID, binding.CurrentTaskName)
	}
	return 0
}

func runInstall(program string, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	global := fs.Bool("global", false, "install into provider home instead of the current workspace")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":    true,
		"--global": false,
		"--json":   false,
	})); err != nil {
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

func runListTasks(program string, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("list-tasks", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	statusRaw := fs.String("status", "", "status filter")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":         true,
		"--description": true,
		"--depends-on":  true,
		"--order":       true,
		"--json":        false,
	})); err != nil {
		return 1
	}
	store, err := Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	planID, err := resolvePlanIDForTaskCommand(store, fs.Args())
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	plan, err := store.GetPlan(planID)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var filter *Status
	if strings.TrimSpace(*statusRaw) != "" {
		value := Status(strings.TrimSpace(*statusRaw))
		if !isValidStatus(value) {
			fmt.Fprintf(stderr, "invalid status: %s\n", value)
			return 1
		}
		filter = &value
	}
	summaries := make([]TaskSummary, 0)
	for _, task := range plan.IterTasksInDisplayOrder() {
		if filter != nil && task.Status != *filter {
			continue
		}
		summaries = append(summaries, summaryFromRecord(task))
	}
	if *jsonOut {
		return writeJSON(stdout, summaries, stderr)
	}
	for _, task := range summaries {
		fmt.Fprintln(stdout, formatTaskSummary(task))
	}
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
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":         true,
		"--description": true,
		"--depends-on":  true,
		"--order":       true,
		"--json":        false,
	})); err != nil {
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s add [--dir PATH] [--description TEXT] [--depends-on a,b] [--order N] [--json] <title>\n", program)
		return 1
	}
	store, err := OpenOrInit(*dir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	var orderPtr *int
	if *order >= 0 {
		value := *order
		orderPtr = &value
	}
	record, err := store.Add(fs.Arg(0), AddOptions{
		Description: *description,
		DependsOn:   splitCSV(*dependsOn),
		Order:       orderPtr,
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

func runClaim(program string, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("claim", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", "", "workspace root")
	by := fs.String("by", "", "actor id")
	jsonOut := fs.Bool("json", false, "emit json")
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":    true,
		"--by":     true,
		"--output": true,
		"--json":   false,
	})); err != nil {
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s claim [--dir PATH] [--by ACTOR] [--json] <id>\n", program)
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
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":    true,
		"--by":     true,
		"--output": true,
		"--json":   false,
	})); err != nil {
		return 1
	}
	if fs.NArg() != 1 {
		fmt.Fprintf(stderr, "usage: %s done [--dir PATH] [--by ACTOR] [--output TEXT] [--json] <id>\n", program)
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
	if err := fs.Parse(reorderFlags(args, map[string]bool{
		"--dir":    true,
		"--by":     true,
		"--reason": true,
		"--json":   false,
	})); err != nil {
		return 1
	}
	if fs.NArg() != 1 || strings.TrimSpace(*reason) == "" {
		fmt.Fprintf(stderr, "usage: %s block [--dir PATH] [--by ACTOR] --reason TEXT [--json] <id>\n", program)
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
	fmt.Fprintf(stdout, "  tasks: %d\n", len(overview.Plan.Tasks))
	if len(overview.Counts) > 0 {
		fmt.Fprintf(stdout, "  counts: %v\n", overview.Counts)
	}
	if overview.Next != nil {
		fmt.Fprintf(stdout, "  next: %s\n", overview.Next.ID)
	}
	for _, task := range overview.Plan.IterTasksInDisplayOrder() {
		fmt.Fprintln(stdout, formatTaskSummary(summaryFromRecord(task)))
	}
	return 0
}

func printUsage(w io.Writer, program string) {
	fmt.Fprintf(w, `Usage:
  %s init [--dir PATH] [--json]
  %s create [--dir PATH] [--description TEXT] [--spec-path PATH] [--json] <name>
  %s list [--dir PATH] [--json]
  %s get [--dir PATH] [--json] [--verbose] <plan>
  %s update [--dir PATH] [--name TEXT] [--description TEXT] [--session-id ID] [--spec-path PATH] [--clear-sessions] [--json] <plan>
  %s delete [--dir PATH] [--yes] [--json] <plan>
  %s add-task [--dir PATH] [--description TEXT] [--parent TASK] [--order N] [--depends-on TASK] [--json] <plan> <name>
  %s update-task [--dir PATH] [--status STATUS] [--name TEXT] [--description TEXT] [--assigned-session ID] [--clear-assigned-session] [--depends-on TASK] [--order N] [--clear-dependencies] [--json] <plan> <task>
  %s delete-task [--dir PATH] [--json] <plan> <task>
  %s ready [--dir PATH] [--json] [plan]
  %s next [--dir PATH] [--session ID] [--json] [plan]
  %s claim-next [--dir PATH] --session ID [--json] [plan]
  %s bind-session [--dir PATH] --session ID [--json] <plan>
  %s bind-current [--dir PATH] [--json] <plan>
  %s install [--global] [--json] <AGENTS.md|CLAUDE.md|skill>

Legacy default-plan aliases:
  %s add [--dir PATH] [--description TEXT] [--depends-on a,b] [--order N] [--json] <title>
  %s claim [--dir PATH] [--by ACTOR] [--json] <id>
  %s done [--dir PATH] [--by ACTOR] [--output TEXT] [--json] <id>
  %s block [--dir PATH] [--by ACTOR] --reason TEXT [--json] <id>
  %s list-tasks [--dir PATH] [--status STATUS] [--json] [plan]
  %s plan show [--dir PATH] [--json]
  %s mcp [--dir PATH]
`, program, program, program, program, program, program, program, program, program, program, program, program, program, program, program, program, program, program, program, program, program, program)
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	return normalizeIDs(parts)
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
		if len(plan.SessionIDs) == 0 {
			lines = append(lines, "Session IDs: (none)")
		} else {
			lines = append(lines, fmt.Sprintf("Session IDs: %s", strings.Join(plan.SessionIDs, ", ")))
		}
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
	folded := strings.ToLower(cleaned)
	casefold := make([]PlanSummary, 0)
	for _, summary := range summaries {
		if strings.ToLower(summary.Name) == folded {
			casefold = append(casefold, summary)
		}
	}
	if len(casefold) == 1 {
		return store.GetPlan(casefold[0].ID)
	}
	if len(casefold) > 1 {
		ids := make([]string, 0, len(casefold))
		for _, summary := range casefold {
			ids = append(ids, summary.ID)
		}
		return nil, fmt.Errorf("multiple plans matched %q: %s", cleaned, strings.Join(ids, ", "))
	}
	return nil, fmt.Errorf("plan not found: %s", cleaned)
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
	folded := strings.ToLower(cleaned)
	casefold := make([]*TaskRecord, 0)
	for i := range plan.Tasks {
		if strings.ToLower(plan.Tasks[i].Name) == folded {
			casefold = append(casefold, &plan.Tasks[i])
		}
	}
	if len(casefold) == 1 {
		return casefold[0], nil
	}
	if len(casefold) > 1 {
		ids := make([]string, 0, len(casefold))
		for _, task := range casefold {
			ids = append(ids, task.ID)
		}
		return nil, fmt.Errorf("multiple tasks matched %q: %s", cleaned, strings.Join(ids, ", "))
	}
	return nil, fmt.Errorf("%s not found: %s", label, cleaned)
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

func resolvePlanIDForTaskCommand(store *Store, args []string) (string, error) {
	if len(args) > 0 {
		plan, err := resolvePlan(store, args[0])
		if err != nil {
			return "", err
		}
		return plan.ID, nil
	}
	workspace, err := store.loadWorkspace()
	if err != nil {
		return "", err
	}
	return workspace.DefaultPlanID, nil
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
		"id":                  task.ID,
		"name":                task.Name,
		"status":              task.Status,
		"assigned_session_id": emptyToNil(task.AssignedSessionID),
		"depends_on":          append([]string(nil), task.DependsOn...),
		"dependency_status":   dependencyStatus,
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
