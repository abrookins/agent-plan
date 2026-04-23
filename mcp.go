package agentplan

import (
	"context"
	"fmt"
	"io"
	"strconv"

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
		Version:         "0.1.0",
		Instructions:    "Filesystem-backed task planning over .agent-plan directories.",
		Provider:        s,
	}
	return server.Serve(ctx, r, w)
}

func (s *MCPServer) Tools(_ context.Context) []mcpproto.Tool {
	rootSchema := map[string]any{
		"type":        "string",
		"description": "Workspace root. Defaults to the server working directory and searches upward for .agent-plan.",
	}
	return []mcpproto.Tool{
		{
			Name:        "plan_init",
			Description: "Initialize a .agent-plan directory in a workspace.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"root": rootSchema,
				},
			},
		},
		{
			Name:        "plan_add",
			Description: "Add a new task to the plan.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"root":        rootSchema,
					"title":       map[string]any{"type": "string"},
					"description": map[string]any{"type": "string"},
					"depends_on": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
					"order": map[string]any{"type": "integer"},
				},
				"required": []string{"title"},
			},
		},
		{
			Name:        "plan_list",
			Description: "List tasks from manifest.json, optionally filtered by status.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"root":   rootSchema,
					"status": map[string]any{"type": "string"},
				},
			},
		},
		{
			Name:        "plan_next",
			Description: "Return the next ready unclaimed task.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"root": rootSchema},
			},
		},
		{
			Name:        "plan_claim",
			Description: "Claim a task by creating its .claim file exclusively.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"root": rootSchema,
					"id":   map[string]any{"type": "string"},
					"by":   map[string]any{"type": "string"},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "plan_done",
			Description: "Complete a task, record output, and move the task file into history.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"root":   rootSchema,
					"id":     map[string]any{"type": "string"},
					"output": map[string]any{"type": "string"},
					"by":     map[string]any{"type": "string"},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:        "plan_block",
			Description: "Mark a task as blocked and clear any claim file.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"root":   rootSchema,
					"id":     map[string]any{"type": "string"},
					"reason": map[string]any{"type": "string"},
					"by":     map[string]any{"type": "string"},
				},
				"required": []string{"id", "reason"},
			},
		},
		{
			Name:        "plan_show",
			Description: "Return a cold-start plan overview with counts and next task.",
			InputSchema: map[string]any{
				"type":       "object",
				"properties": map[string]any{"root": rootSchema},
			},
		},
	}
}

func (s *MCPServer) CallTool(_ context.Context, name string, args map[string]any) mcpproto.ToolResult {
	root := s.resolveRoot(args)

	switch name {
	case "plan_init":
		store, err := Init(root)
		if err != nil {
			return toolErr(err)
		}
		return toolOK(fmt.Sprintf("initialized %s", store.MetaDir), map[string]any{
			"root":     store.Root,
			"meta_dir": store.MetaDir,
		})
	case "plan_add":
		title, err := requiredString(args, "title")
		if err != nil {
			return toolErr(err)
		}
		store, err := OpenOrInit(root)
		if err != nil {
			return toolErr(err)
		}
		record, err := store.Add(title, AddOptions{
			Description: optionalString(args, "description"),
			DependsOn:   optionalStringSlice(args, "depends_on"),
			Order:       optionalIntPointer(args, "order"),
		})
		if err != nil {
			return toolErr(err)
		}
		return toolOK("task added", record)
	case "plan_list":
		store, err := Open(root)
		if err != nil {
			return toolErr(err)
		}
		opts := ListOptions{}
		if raw := optionalString(args, "status"); raw != "" {
			status := Status(raw)
			opts.Status = &status
		}
		tasks, err := store.List(opts)
		if err != nil {
			return toolErr(err)
		}
		return toolOK(fmt.Sprintf("listed %d tasks", len(tasks)), tasks)
	case "plan_next":
		store, err := Open(root)
		if err != nil {
			return toolErr(err)
		}
		task, err := store.Next()
		if err != nil {
			return toolErr(err)
		}
		if task == nil {
			return toolOK("no ready task", map[string]any{"task": nil})
		}
		return toolOK("next task selected", task)
	case "plan_claim":
		id, err := requiredString(args, "id")
		if err != nil {
			return toolErr(err)
		}
		store, err := Open(root)
		if err != nil {
			return toolErr(err)
		}
		record, err := store.Claim(id, optionalString(args, "by"))
		if err != nil {
			return toolErr(err)
		}
		return toolOK("task claimed", record)
	case "plan_done":
		id, err := requiredString(args, "id")
		if err != nil {
			return toolErr(err)
		}
		store, err := Open(root)
		if err != nil {
			return toolErr(err)
		}
		record, err := store.Done(id, optionalString(args, "output"), optionalString(args, "by"))
		if err != nil {
			return toolErr(err)
		}
		return toolOK("task completed", record)
	case "plan_block":
		id, err := requiredString(args, "id")
		if err != nil {
			return toolErr(err)
		}
		reason, err := requiredString(args, "reason")
		if err != nil {
			return toolErr(err)
		}
		store, err := Open(root)
		if err != nil {
			return toolErr(err)
		}
		record, err := store.Block(id, reason, optionalString(args, "by"))
		if err != nil {
			return toolErr(err)
		}
		return toolOK("task blocked", record)
	case "plan_show":
		store, err := Open(root)
		if err != nil {
			return toolErr(err)
		}
		overview, err := store.Overview()
		if err != nil {
			return toolErr(err)
		}
		return toolOK("plan loaded", overview)
	default:
		return toolErr(fmt.Errorf("unknown tool: %s", name))
	}
}

func runMCP(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	_ = stderr
	root := ""
	if len(args) > 0 {
		root = args[0]
	}
	server := NewMCPServer(root)
	if err := server.Serve(ctx, stdin, stdout); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func (s *MCPServer) resolveRoot(args map[string]any) string {
	if raw := optionalString(args, "root"); raw != "" {
		return raw
	}
	return s.BaseDir
}

func toolOK(message string, data any) mcpproto.ToolResult {
	return mcpproto.ToolResult{
		Content:           []mcpproto.TextContent{{Type: "text", Text: message}},
		StructuredContent: data,
	}
}

func toolErr(err error) mcpproto.ToolResult {
	return mcpproto.ToolResult{
		Content: []mcpproto.TextContent{{Type: "text", Text: err.Error()}},
		IsError: true,
	}
}

func requiredString(args map[string]any, key string) (string, error) {
	value := optionalString(args, key)
	if value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func optionalString(args map[string]any, key string) string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return value
	default:
		return fmt.Sprint(value)
	}
}

func optionalStringSlice(args map[string]any, key string) []string {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	switch values := raw.(type) {
	case []string:
		return values
	case []any:
		result := make([]string, 0, len(values))
		for _, value := range values {
			result = append(result, fmt.Sprint(value))
		}
		return result
	default:
		return nil
	}
}

func optionalIntPointer(args map[string]any, key string) *int {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	switch value := raw.(type) {
	case int:
		return &value
	case int32:
		parsed := int(value)
		return &parsed
	case int64:
		parsed := int(value)
		return &parsed
	case float64:
		parsed := int(value)
		return &parsed
	case string:
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return nil
		}
		return &parsed
	default:
		return nil
	}
}
