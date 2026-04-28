package agentplan

import (
	"context"
	"encoding/json"
	"testing"
)

func TestMCPCallToolCoverage(t *testing.T) {
	root := t.TempDir()
	server := NewMCPServer(root)

	cases := []struct {
		name      string
		args      map[string]any
		wantError bool
	}{
		{"plan_init", map[string]any{}, false},
		{"plan_add", map[string]any{"title": "Task"}, false},
		{"plan_list", map[string]any{}, false},
		{"plan_next", map[string]any{}, false},
		{"plan_claim", map[string]any{"id": "001-task"}, false},
		{"plan_done", map[string]any{"id": "001-task", "output": "done"}, false},
		{"plan_block", map[string]any{"id": "001-task"}, true},
		{"plan_show", map[string]any{}, false},
		{"unknown", map[string]any{}, true},
	}

	for _, tc := range cases {
		result := server.CallTool(context.Background(), tc.name, tc.args)
		if result.IsError != tc.wantError {
			t.Fatalf("%s error=%v want=%v result=%#v", tc.name, result.IsError, tc.wantError, result)
		}
	}

	result := server.CallTool(context.Background(), "plan_list", map[string]any{"status": string(StatusComplete)})
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var tasks []TaskSummary
	if err := json.Unmarshal(raw, &tasks); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if len(tasks) != 1 || tasks[0].Status != StatusComplete {
		t.Fatalf("tasks = %#v", tasks)
	}

	if result := server.CallTool(context.Background(), "plan_add", map[string]any{}); !result.IsError {
		t.Fatal("plan_add without title should fail")
	}
}
