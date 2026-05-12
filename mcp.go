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
		Version:         "0.2.0",
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
				"plan":           planSchema,
				"name":           map[string]any{"type": "string"},
				"description":    map[string]any{"type": "string"},
				"spec_path":      map[string]any{"type": "string"},
				"session_ids":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"clear_sessions": map[string]any{"type": "boolean"},
			}, "plan"),
		},
		{
			Name:        "plan_delete",
			Description: "Delete a plan from the workspace store.",
			InputSchema: schema(rootSchema, map[string]any{"plan": planSchema}, "plan"),
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
				"plan":                   planSchema,
				"task":                   taskSchema,
				"status":                 map[string]any{"type": "string"},
				"name":                   map[string]any{"type": "string"},
				"description":            map[string]any{"type": "string"},
				"assigned_session_id":    map[string]any{"type": "string"},
				"clear_assigned_session": map[string]any{"type": "boolean"},
				"depends_on":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"clear_dependencies":     map[string]any{"type": "boolean"},
				"order":                  map[string]any{"type": "integer"},
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
			Description: "Select the next actionable task, optionally preferring a session.",
			InputSchema: schema(rootSchema, map[string]any{
				"plan":    planSchema,
				"session": map[string]any{"type": "string"},
			}, "plan"),
		},
		{
			Name:        "plan_claim_next",
			Description: "Claim the next actionable task for a session.",
			InputSchema: schema(rootSchema, map[string]any{
				"plan":    planSchema,
				"session": map[string]any{"type": "string"},
			}, "plan", "session"),
		},
		{
			Name:        "plan_bind_session",
			Description: "Bind a session to a plan in the local workspace metadata.",
			InputSchema: schema(rootSchema, map[string]any{
				"plan":    planSchema,
				"session": map[string]any{"type": "string"},
			}, "plan", "session"),
		},
		{
			Name:        "plan_bind_current",
			Description: "Bind the current runtime session to a plan using environment variables.",
			InputSchema: schema(rootSchema, map[string]any{"plan": planSchema}, "plan"),
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
		task := plan.SelectNextTask(optionalString(args, "session"))
		if task == nil {
			return toolOK("no ready task", map[string]any{"task": nil})
		}
		return toolOK("next task selected", taskPayload(plan, *task))
	case "plan_claim_next":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		record, err := store.ClaimNext(plan.ID, optionalString(args, "session"))
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
	case "plan_bind_session":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		binding, err := store.BindSession(plan.ID, optionalString(args, "session"))
		if err != nil {
			return toolErr(err)
		}
		return toolOK("session bound", binding)
	case "plan_bind_current":
		plan, err := resolvePlanFromArgs(store, args)
		if err != nil {
			return toolErr(err)
		}
		binding, err := store.BindCurrent(plan.ID)
		if err != nil {
			return toolErr(err)
		}
		return toolOK("current session bound", binding)
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
	opts := PlanUpdateOptions{
		SessionIDs:    optionalStringSlice(args, "session_ids"),
		ClearSessions: optionalBool(args, "clear_sessions"),
	}
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

func taskUpdateOptionsFromArgs(plan *Plan, taskID string, args map[string]any) (TaskUpdateOptions, error) {
	opts := TaskUpdateOptions{
		ClearAssignedSession: optionalBool(args, "clear_assigned_session"),
		ClearDependencies:    optionalBool(args, "clear_dependencies"),
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
	if _, ok := args["assigned_session_id"]; ok {
		value := optionalString(args, "assigned_session_id")
		opts.AssignedSessionID = &value
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
