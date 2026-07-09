package agentplan

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/andrewbrookins/agent-plan/internal/mcpproto"
)

const protocolVersion = "2024-11-05"

type MCPServer struct {
	BaseDir string
}

func NewMCPServer(baseDir string) *MCPServer {
	return &MCPServer{BaseDir: baseDir}
}

func (s *MCPServer) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	server := &mcpproto.Server{
		ProtocolVersion: protocolVersion,
		Name:            "agent-plan",
		Version:         Version,
		Instructions:    "Filesystem-backed shared plan management over workspace-local .agent-plan directories.",
		Provider:        s,
	}
	return server.Serve(ctx, r, w)
}

func (s *MCPServer) Tools(_ context.Context) []mcpproto.Tool {
	rootSchema := map[string]any{
		"type":        "string",
		"description": "Workspace root. Defaults to the AGENT_PLAN_DIR env var when set, otherwise the server working directory and searches upward for .agent-plan.",
	}
	planSchema := map[string]any{"type": "string"}
	taskSchema := map[string]any{"type": "string"}
	return []mcpproto.Tool{
		{
			Name:        "plan_init",
			Description: "Initialize a workspace-local .agent-plan store.",
			InputSchema: schema(rootSchema, nil),
		},
		{
			Name:        "plan_create",
			Description: "Create a new plan in the workspace store.",
			InputSchema: schema(rootSchema, map[string]any{
				"name":        map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
				"spec_path":   map[string]any{"type": "string"},
			}, "name"),
		},
		{
			Name:        "plan_list",
			Description: "List plans in the workspace store.",
			InputSchema: schema(rootSchema, nil),
		},
		{
			Name:        "plan_get",
			Description: "Load a plan and its tasks.",
			InputSchema: schema(rootSchema, map[string]any{"plan": planSchema}, "plan"),
		},
		{
			Name:        "plan_show",
			Description: "Show the current plan state and all tasks.",
			InputSchema: schema(rootSchema, map[string]any{"plan": planSchema}, "plan"),
		},
		{
			Name:        "plan_update",
			Description: "Update plan metadata.",
			InputSchema: schema(rootSchema, map[string]any{
				"plan":        planSchema,
				"name":        map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
				"spec_path":   map[string]any{"type": "string"},
			}, "plan"),
		},
		{
			Name:        "plan_delete",
			Description: "Delete a plan from the workspace store.",
			InputSchema: schema(rootSchema, map[string]any{"plan": planSchema}, "plan"),
		},
		{
			Name:        "plan_import",
			Description: "Validate and import a structured task graph into a plan.",
			InputSchema: schema(rootSchema, map[string]any{
				"plan":    planSchema,
				"spec":    map[string]any{"type": "object"},
				"dry_run": map[string]any{"type": "boolean"},
			}, "plan", "spec"),
		},
		{
			Name:        "plan_stats",
			Description: "Show compact plan counts without returning every task.",
			InputSchema: schema(rootSchema, map[string]any{"plan": planSchema}, "plan"),
		},
		{
			Name:        "plan_audit",
			Description: "Audit a plan for structural and coordination problems.",
			InputSchema: schema(rootSchema, map[string]any{"plan": planSchema}, "plan"),
		},
		{
			Name:        "plan_lock_status",
			Description: "Inspect a plan lock.",
			InputSchema: schema(rootSchema, map[string]any{"plan": planSchema}, "plan"),
		},
		{
			Name:        "plan_lock_clear",
			Description: "Clear a plan lock only when if_dead is true and the owner is locally proven dead.",
			InputSchema: schema(rootSchema, map[string]any{
				"plan":    planSchema,
				"if_dead": map[string]any{"type": "boolean"},
			}, "plan", "if_dead"),
		},
		{
			Name:        "plan_chain",
			Description: "Add dependency edges so each listed task depends on the previous one.",
			InputSchema: schema(rootSchema, map[string]any{
				"plan":  planSchema,
				"tasks": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			}, "plan", "tasks"),
		},
		{
			Name:        "plan_add_task",
			Description: "Add a task to a plan.",
			InputSchema: schema(rootSchema, map[string]any{
				"plan":        planSchema,
				"name":        map[string]any{"type": "string"},
				"description": map[string]any{"type": "string"},
				"parent":      taskSchema,
				"depends_on":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"order":       map[string]any{"type": "integer"},
			}, "plan", "name"),
		},
		{
			Name:        "plan_update_task",
			Description: "Update task status or details.",
			InputSchema: schema(rootSchema, map[string]any{
				"plan":               planSchema,
				"task":               taskSchema,
				"status":             map[string]any{"type": "string"},
				"name":               map[string]any{"type": "string"},
				"description":        map[string]any{"type": "string"},
				"parent":             taskSchema,
				"claimed_by":         map[string]any{"type": "string"},
				"clear_claimed_by":   map[string]any{"type": "boolean"},
				"depends_on":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"clear_dependencies": map[string]any{"type": "boolean"},
				"order":              map[string]any{"type": "integer"},
			}, "plan", "task"),
		},
		{
			Name:        "plan_delete_task",
			Description: "Delete a task and any descendants from a plan.",
			InputSchema: schema(rootSchema, map[string]any{"plan": planSchema, "task": taskSchema}, "plan", "task"),
		},
		{
			Name:        "plan_ready",
			Description: "List actionable tasks whose dependencies are complete.",
			InputSchema: schema(rootSchema, map[string]any{"plan": planSchema}, "plan"),
		},
		{
			Name:        "plan_next",
			Description: "Select the next actionable task, optionally preferring tasks already claimed by a specific agent id.",
			InputSchema: schema(rootSchema, map[string]any{
				"plan": planSchema,
				"by":   map[string]any{"type": "string"},
			}, "plan"),
		},
		{
			Name:        "plan_claim_next",
			Description: "Claim the next actionable task, optionally recording a specific agent id as the claimer.",
			InputSchema: schema(rootSchema, map[string]any{
				"plan": planSchema,
				"by":   map[string]any{"type": "string"},
			}, "plan"),
		},
		{
			Name:        "plan_install",
			Description: "Install managed plan guidance into AGENTS.md, CLAUDE.md, or a skill.",
			InputSchema: schema(rootSchema, map[string]any{
				"target": map[string]any{"type": "string"},
				"global": map[string]any{"type": "boolean"},
			}, "target"),
		},
	}
}

func (s *MCPServer) CallTool(_ context.Context, name string, args map[string]any) mcpproto.ToolResult {
	root := s.resolveRoot(args)
	store, err := OpenOrInit(root)
	if err != nil {
		return toolErr(err)
	}

	switch name {
	case "plan_init":
		return toolOK(fmt.Sprintf("initialized %s", store.MetaDir), map[string]any{
			"root":     store.Root,
			"meta_dir": store.MetaDir,
		})
	case "plan_create":
		plan, err := store.CreatePlan(optionalString(args, "name"), optionalString(args, "description"), optionalString(args, "spec_path"))
		if err != nil {
			return toolErr(err)
		}
		return toolOK("plan created", plan)
	case "plan_list":
		plans, err := store.ListPlans()
		if err != nil {
			return toolErr(err)
		}
		return toolOK(fmt.Sprintf("listed %d plans", len(plans)), plans)
	case "plan_get":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		return toolOK("plan loaded", plan)
	case "plan_show":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		return toolOK("plan state loaded", plan)
	case "plan_update":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		opts, err := planUpdateOptionsFromArgs(args)
		if err != nil {
			return toolErr(err)
		}
		updated, err := store.UpdatePlan(plan.ID, opts)
		if err != nil {
			return toolErr(err)
		}
		return toolOK("plan updated", updated)
	case "plan_delete":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		deleted, err := store.DeletePlan(plan.ID)
		if err != nil {
			return toolErr(err)
		}
		return toolOK("plan deleted", map[string]any{"deleted": deleted, "id": plan.ID})
	case "plan_import":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		spec, err := bulkImportSpecFromArgs(args)
		if err != nil {
			return toolErr(err)
		}
		report, err := store.ImportTasks(plan.ID, spec, BulkImportOptions{DryRun: optionalBool(args, "dry_run")})
		if err != nil {
			if report != nil {
				return mcpproto.ToolResult{
					IsError:           true,
					StructuredContent: report,
					Content: []mcpproto.TextContent{
						{Type: "text", Text: err.Error()},
					},
				}
			}
			return toolErr(err)
		}
		return toolOK("plan imported", report)
	case "plan_stats":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		stats, err := store.Stats(plan.ID)
		if err != nil {
			return toolErr(err)
		}
		return toolOK("plan stats loaded", stats)
	case "plan_audit":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		report, err := store.Audit(plan.ID)
		if err != nil {
			return toolErr(err)
		}
		return toolOK("plan audit loaded", report)
	case "plan_lock_status":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		status, err := store.PlanLockStatus(plan.ID)
		if err != nil {
			return toolErr(err)
		}
		return toolOK("plan lock status loaded", status)
	case "plan_lock_clear":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		if !optionalBool(args, "if_dead") {
			return toolErr(fmt.Errorf("plan_lock_clear requires if_dead=true"))
		}
		result, err := store.ClearPlanLockIfDead(plan.ID)
		if err != nil {
			return toolErr(err)
		}
		return toolOK("plan lock clear attempted", result)
	case "plan_chain":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		taskIDs := make([]string, 0)
		for _, ref := range optionalStringSlice(args, "tasks") {
			task, err := resolveTask(plan, ref, "task")
			if err != nil {
				return toolErr(err)
			}
			taskIDs = append(taskIDs, task.ID)
		}
		updated, err := store.ChainTasks(plan.ID, taskIDs)
		if err != nil {
			return toolErr(err)
		}
		return toolOK("tasks chained", updated)
	case "plan_add_task":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		parentID := ""
		if parentRef := optionalString(args, "parent"); parentRef != "" {
			parentTask, err := resolveTask(plan, parentRef, "parent task")
			if err != nil {
				return toolErr(err)
			}
			parentID = parentTask.ID
		}
		deps, err := resolveDependencyIDs(plan, optionalStringSlice(args, "depends_on"))
		if err != nil {
			return toolErr(err)
		}
		record, err := store.AddTask(plan.ID, optionalString(args, "name"), AddOptions{
			Description: optionalString(args, "description"),
			ParentID:    parentID,
			DependsOn:   deps,
			Order:       optionalIntPointer(args, "order"),
		})
		if err != nil {
			return toolErr(err)
		}
		return toolOK("task added", record)
	case "plan_update_task":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		task, err := resolveTaskFromArgs(plan, args, "task")
		if err != nil {
			return toolErr(err)
		}
		opts, err := taskUpdateOptionsFromArgs(plan, task.ID, args)
		if err != nil {
			return toolErr(err)
		}
		record, err := store.UpdateTask(plan.ID, task.ID, opts)
		if err != nil {
			return toolErr(err)
		}
		return toolOK("task updated", record)
	case "plan_delete_task":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		task, err := resolveTaskFromArgs(plan, args, "task")
		if err != nil {
			return toolErr(err)
		}
		deleted, err := store.DeleteTask(plan.ID, task.ID)
		if err != nil {
			return toolErr(err)
		}
		return toolOK("task deleted", map[string]any{"deleted": deleted, "id": task.ID})
	case "plan_ready":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		ready := plan.ReadyTasks()
		payload := make([]map[string]any, 0, len(ready))
		for _, task := range ready {
			payload = append(payload, taskPayload(plan, task))
		}
		return toolOK(fmt.Sprintf("listed %d ready tasks", len(ready)), payload)
	case "plan_next":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		task := plan.SelectNextTask(optionalString(args, "by"))
		if task == nil {
			return toolOK("no ready task", map[string]any{"task": nil})
		}
		return toolOK("next task selected", taskPayload(plan, *task))
	case "plan_claim_next":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		record, err := store.ClaimNext(plan.ID, optionalString(args, "by"))
		if err != nil {
			return toolErr(err)
		}
		if record == nil {
			return toolOK("no ready task", map[string]any{"task": nil})
		}
		updatedPlan, err := store.GetPlan(plan.ID)
		if err != nil {
			return toolErr(err)
		}
		return toolOK("task claimed", taskPayload(updatedPlan, *record))
	case "plan_install":
		path, err := InstallGuidance(optionalString(args, "target"), optionalBool(args, "global"), store.Root)
		if err != nil {
			return toolErr(err)
		}
		return toolOK("guidance installed", map[string]any{
			"target": optionalString(args, "target"),
			"scope":  ternary(optionalBool(args, "global"), "global", "workspace"),
			"path":   path,
		})
	default:
		return toolErr(fmt.Errorf("unknown tool: %s", name))
	}
}

func runMCP(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	_ = stderr
	root := ""
	if len(args) >= 2 && args[0] == "--dir" {
		root = args[1]
	}
	server := NewMCPServer(root)
	if err := server.Serve(ctx, stdin, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func (s *MCPServer) resolveRoot(args map[string]any) string {
	root := optionalString(args, "root")
	if root != "" {
		return root
	}
	return s.BaseDir
}

func schema(root map[string]any, extra map[string]any, required ...string) map[string]any {
	properties := map[string]any{"root": root}
	for key, value := range extra {
		properties[key] = value
	}
	result := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		result["required"] = required
	}
	return result
}

func optionalString(args map[string]any, key string) string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return value
}

func optionalStringSlice(args map[string]any, key string) []string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		if value, ok := item.(string); ok {
			values = append(values, value)
		}
	}
	return values
}

func optionalIntPointer(args map[string]any, key string) *int {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	switch value := raw.(type) {
	case float64:
		cast := int(value)
		return &cast
	case int:
		cast := value
		return &cast
	default:
		return nil
	}
}

func optionalBool(args map[string]any, key string) bool {
	raw, ok := args[key]
	if !ok || raw == nil {
		return false
	}
	value, ok := raw.(bool)
	return ok && value
}

func resolvePlanFromArgs(store *Store, args map[string]any) (*Plan, error) {
	return resolvePlan(store, optionalString(args, "plan"))
}

func resolveTaskFromArgs(plan *Plan, args map[string]any, key string) (*TaskRecord, error) {
	return resolveTask(plan, optionalString(args, key), key)
}

func planUpdateOptionsFromArgs(args map[string]any) (PlanUpdateOptions, error) {
	opts := PlanUpdateOptions{}
	if _, ok := args["name"]; ok {
		value := optionalString(args, "name")
		opts.Name = &value
	}
	if _, ok := args["description"]; ok {
		value := optionalString(args, "description")
		opts.Description = &value
	}
	if _, ok := args["spec_path"]; ok {
		value := optionalString(args, "spec_path")
		opts.SpecPath = &value
	}
	return opts, nil
}

func bulkImportSpecFromArgs(args map[string]any) (BulkImportSpec, error) {
	raw, ok := args["spec"]
	if !ok || raw == nil {
		return BulkImportSpec{}, fmt.Errorf("spec is required")
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return BulkImportSpec{}, err
	}
	var spec BulkImportSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return BulkImportSpec{}, err
	}
	return spec, nil
}

func taskUpdateOptionsFromArgs(plan *Plan, taskID string, args map[string]any) (TaskUpdateOptions, error) {
	opts := TaskUpdateOptions{
		ClearClaimedBy:    optionalBool(args, "clear_claimed_by"),
		ClearDependencies: optionalBool(args, "clear_dependencies"),
	}
	if _, ok := args["status"]; ok {
		value := Status(optionalString(args, "status"))
		if !isValidStatus(value) {
			return TaskUpdateOptions{}, fmt.Errorf("invalid status: %s", value)
		}
		opts.Status = &value
	}
	if _, ok := args["name"]; ok {
		value := optionalString(args, "name")
		opts.Name = &value
	}
	if _, ok := args["description"]; ok {
		value := optionalString(args, "description")
		opts.Description = &value
	}
	if _, ok := args["parent"]; ok {
		value := ""
		if parentRef := optionalString(args, "parent"); parentRef != "" {
			parentTask, err := resolveTask(plan, parentRef, "parent task")
			if err != nil {
				return TaskUpdateOptions{}, err
			}
			value = parentTask.ID
		}
		opts.ParentID = &value
	}
	if _, ok := args["claimed_by"]; ok {
		value := optionalString(args, "claimed_by")
		opts.ClaimedBy = &value
	}
	if deps := optionalStringSlice(args, "depends_on"); deps != nil {
		resolved := make([]string, 0, len(deps))
		for _, ref := range deps {
			task, err := resolveTask(plan, ref, "dependency task")
			if err != nil {
				return TaskUpdateOptions{}, err
			}
			resolved = append(resolved, task.ID)
		}
		normalized, err := plan.ValidateDependencyIDs(resolved, taskID)
		if err != nil {
			return TaskUpdateOptions{}, err
		}
		opts.DependsOn = normalized
	}
	opts.Order = optionalIntPointer(args, "order")
	return opts, nil
}

func toolOK(message string, value any) mcpproto.ToolResult {
	payload, _ := json.MarshalIndent(value, "", "  ")
	return mcpproto.ToolResult{
		StructuredContent: value,
		Content: []mcpproto.TextContent{
			{Type: "text", Text: message + "\n" + string(payload)},
		},
	}
}

func toolErr(err error) mcpproto.ToolResult {
	return mcpproto.ToolResult{
		IsError: true,
		Content: []mcpproto.TextContent{
			{Type: "text", Text: err.Error()},
		},
	}
}
