package agentplan

import (
	"context"
	"encoding/json"
	"os"
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
		"plan":       plan.ID,
		"task":       spec.ID,
		"claimed_by": "agent-mcp",
	})
	if result.IsError {
		t.Fatalf("plan_update_task result = %#v", result)
	}

	result = server.CallTool(context.Background(), "plan_claim_next", map[string]any{
		"plan": plan.ID,
		"by":   "agent-mcp",
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

	result = server.CallTool(context.Background(), "plan_show", map[string]any{"plan": plan.ID})
	if result.IsError {
		t.Fatalf("plan_show result = %#v", result)
	}
	raw, err = json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(plan_show) error = %v", err)
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatalf("Unmarshal(plan_show) error = %v", err)
	}
	if len(plan.Tasks) != 2 {
		t.Fatalf("plan_show plan = %#v", plan)
	}

	result = server.CallTool(context.Background(), "plan_import", map[string]any{
		"plan": plan.ID,
		"spec": map[string]any{
			"tasks": []any{
				map[string]any{"key": "verify", "name": "Verify", "depends_on": []any{"Implementation"}},
			},
		},
	})
	if result.IsError {
		t.Fatalf("plan_import result = %#v", result)
	}
	raw, err = json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(plan_import) error = %v", err)
	}
	var report BulkImportReport
	if err := json.Unmarshal(raw, &report); err != nil {
		t.Fatalf("Unmarshal(plan_import) error = %v", err)
	}
	if report.Created != 1 {
		t.Fatalf("plan_import report = %#v", report)
	}

	result = server.CallTool(context.Background(), "plan_stats", map[string]any{"plan": plan.ID})
	if result.IsError {
		t.Fatalf("plan_stats result = %#v", result)
	}
	raw, err = json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal(plan_stats) error = %v", err)
	}
	var stats PlanStats
	if err := json.Unmarshal(raw, &stats); err != nil {
		t.Fatalf("Unmarshal(plan_stats) error = %v", err)
	}
	if stats.Total != 3 {
		t.Fatalf("plan_stats = %#v", stats)
	}

	result = server.CallTool(context.Background(), "plan_chain", map[string]any{
		"plan":  plan.ID,
		"tasks": []any{"Spec", "Implementation", "Verify"},
	})
	if result.IsError {
		t.Fatalf("plan_chain result = %#v", result)
	}

	result = server.CallTool(context.Background(), "plan_audit", map[string]any{"plan": plan.ID})
	if result.IsError {
		t.Fatalf("plan_audit result = %#v", result)
	}

	store, err := Open(root)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := os.WriteFile(store.planLockPath(plan.ID), []byte(`{"pid":999999999,"host":"","created_at":"2026-01-01T00:00:00Z"}`), defaultFilePerm); err != nil {
		t.Fatalf("WriteFile(lock) error = %v", err)
	}
	result = server.CallTool(context.Background(), "plan_lock_status", map[string]any{"plan": plan.ID})
	if result.IsError {
		t.Fatalf("plan_lock_status result = %#v", result)
	}
	result = server.CallTool(context.Background(), "plan_lock_clear", map[string]any{"plan": plan.ID, "if_dead": true})
	if result.IsError {
		t.Fatalf("plan_lock_clear result = %#v", result)
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
