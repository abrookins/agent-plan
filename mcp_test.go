package agentplan

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPPlanLifecycle(t *testing.T) {
	root := t.TempDir()
	server := NewMCPServer(root)

	result := server.CallTool(context.Background(), "plan_init", map[string]any{})
	if result.IsError {
		t.Fatalf("plan_init result = %#v", result)
	}

	result = server.CallTool(context.Background(), "plan_create", map[string]any{
		"name":        "MCP Plan",
		"description": "Track MCP parity",
	})
	if result.IsError {
		t.Fatalf("plan_create result = %#v", result)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(plan_create) error = %v", err)
	}
	var plan Plan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("Unmarshal(plan_create) error = %v", err)
	}

	result = server.CallTool(context.Background(), "plan_add_task", map[string]any{
		"plan": plan.ID,
		"name": "Spec",
	})
	if result.IsError {
		t.Fatalf("plan_add_task result = %#v", result)
	}
	raw, err = json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(plan_add_task) error = %v", err)
	}
	var spec TaskRecord
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("Unmarshal(plan_add_task) error = %v", err)
	}

	result = server.CallTool(context.Background(), "plan_add_task", map[string]any{
		"plan":       plan.ID,
		"name":       "Implementation",
		"depends_on": []any{spec.ID},
	})
	if result.IsError {
		t.Fatalf("plan_add_task(dep) result = %#v", result)
	}

	result = server.CallTool(context.Background(), "plan_ready", map[string]any{"plan": plan.ID})
	if result.IsError {
		t.Fatalf("plan_ready result = %#v", result)
	}
	raw, err = json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(plan_ready) error = %v", err)
	}
	var ready []map[string]any
	if err := json.Unmarshal(raw, &ready); err != nil {
		t.Fatalf("Unmarshal(plan_ready) error = %v", err)
	}
	if len(ready) != 1 || ready[0]["id"] != spec.ID {
		t.Fatalf("ready = %#v", ready)
	}

	result = server.CallTool(context.Background(), "plan_update_task", map[string]any{
		"plan":                plan.ID,
		"task":                spec.ID,
		"assigned_session_id": "sess-mcp",
	})
	if result.IsError {
		t.Fatalf("plan_update_task result = %#v", result)
	}

	result = server.CallTool(context.Background(), "plan_bind_session", map[string]any{
		"plan":    plan.ID,
		"session": "sess-mcp",
	})
	if result.IsError {
		t.Fatalf("plan_bind_session result = %#v", result)
	}

	result = server.CallTool(context.Background(), "plan_claim_next", map[string]any{
		"plan":    plan.ID,
		"session": "sess-mcp",
	})
	if result.IsError {
		t.Fatalf("plan_claim_next result = %#v", result)
	}
	if !strings.Contains(result.Content[0].Text, spec.ID) {
		t.Fatalf("plan_claim_next content = %#v", result.Content)
	}

	result = server.CallTool(context.Background(), "plan_install", map[string]any{
		"target": "skill",
	})
	if result.IsError {
		t.Fatalf("plan_install result = %#v", result)
	}

	result = server.CallTool(context.Background(), "plan_get", map[string]any{"plan": plan.ID})
	if result.IsError {
		t.Fatalf("plan_get result = %#v", result)
	}
	raw, err = json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(plan_get) error = %v", err)
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("Unmarshal(plan_get) error = %v", err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("plan = %#v", plan)
	}

	result = server.CallTool(context.Background(), "plan_delete", map[string]any{"plan": plan.ID})
	if result.IsError {
		t.Fatalf("plan_delete result = %#v", result)
	}
}

func TestMCPUnknownToolErrors(t *testing.T) {
	root := t.TempDir()
	server := NewMCPServer(root)

	if result := server.CallTool(context.Background(), "unknown", map[string]any{}); !result.IsError {
		t.Fatal("unknown tool should fail")
	}
}

func TestMCPUpdateTaskOrderReturnsUpdatedTask(t *testing.T) {
	root := t.TempDir()
	server := NewMCPServer(root)

	if result := server.CallTool(context.Background(), "plan_init", map[string]any{}); result.IsError {
		t.Fatalf("plan_init result = %#v", result)
	}
	create := server.CallTool(context.Background(), "plan_create", map[string]any{"name": "Order Plan"})
	if create.IsError {
		t.Fatalf("plan_create result = %#v", create)
	}
	raw, err := json.Marshal(create.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(plan_create) error = %v", err)
	}
	var plan Plan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("Unmarshal(plan_create) error = %v", err)
	}

	first := server.CallTool(context.Background(), "plan_add_task", map[string]any{"plan": plan.ID, "name": "A"})
	if first.IsError {
		t.Fatalf("plan_add_task(A) result = %#v", first)
	}
	raw, err = json.Marshal(first.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(plan_add_task A) error = %v", err)
	}
	var taskA TaskRecord
	if err := json.Unmarshal(raw, &taskA); err != nil {
		t.Fatalf("Unmarshal(plan_add_task A) error = %v", err)
	}

	second := server.CallTool(context.Background(), "plan_add_task", map[string]any{"plan": plan.ID, "name": "B"})
	if second.IsError {
		t.Fatalf("plan_add_task(B) result = %#v", second)
	}

	update := server.CallTool(context.Background(), "plan_update_task", map[string]any{
		"plan":  plan.ID,
		"task":  taskA.ID,
		"order": 50,
	})
	if update.IsError {
		t.Fatalf("plan_update_task result = %#v", update)
	}
	raw, err = json.Marshal(update.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(plan_update_task) error = %v", err)
	}
	var updated TaskRecord
	if err := json.Unmarshal(raw, &updated); err != nil {
		t.Fatalf("Unmarshal(plan_update_task) error = %v", err)
	}
	if updated.ID != taskA.ID {
		t.Fatalf("updated task id = %q, want %q", updated.ID, taskA.ID)
	}
}
