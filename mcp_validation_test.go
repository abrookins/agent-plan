package agentplan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPArgumentValidationBeforeStorage(t *testing.T) {
	root := filepath.Join(t.TempDir(), "untouched")
	server := NewMCPServer(root)
	for _, test := range []struct {
		name string
		args map[string]any
	}{
		{"plan_update_task", map[string]any{"plan": "missing", "task": "missing", "status": "private-invalid-value"}},
		{"plan_get_tasks", map[string]any{"plan": "missing", "status": []any{"private-invalid-value"}}},
		{"plan_get_tasks", map[string]any{"plan": "missing"}},
		{"plan_get_tasks", map[string]any{"plan": "missing", "tasks": []any{}}},
		{"plan_get_tasks", map[string]any{"plan": "missing", "status": []any{}}},
		{"plan_get_tasks", map[string]any{"plan": "missing", "parent": ""}},
		{"plan_get_tasks", map[string]any{"plan": "missing", "claimed_by": ""}},
		{"plan_get", map[string]any{}},
		{"plan_get", map[string]any{"plan": 123}},
		{"plan_get", map[string]any{"plan": "missing", "typo": "private-invalid-value"}},
		{"unknown", nil},
	} {
		result := server.CallTool(context.Background(), test.name, test.args)
		if !result.IsError {
			t.Fatalf("accepted %s", test.name)
		}
		if strings.Contains(result.Content[0].Text, "private-invalid-value") {
			t.Fatal("echoed argument value")
		}
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("invalid calls touched storage: %v", err)
	}
	for _, status := range []Status{StatusNotStarted, StatusInProgress, StatusBlocked, StatusComplete, StatusCancelled} {
		if err := server.validateArguments(context.Background(), "plan_update_task", map[string]any{"plan": "p", "task": "t", "status": string(status)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range []map[string]any{
		{"plan": "p", "tasks": []any{"t"}},
		{"plan": "p", "status": []any{string(StatusNotStarted)}},
		{"plan": "p", "parent": "t"},
		{"plan": "p", "claimed_by": "agent"},
	} {
		if err := server.validateArguments(context.Background(), "plan_get_tasks", args); err != nil {
			t.Fatal(err)
		}
	}
}
