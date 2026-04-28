package agentplan

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type failingWriter struct{ err error }

func (w failingWriter) Write(_ []byte) (int, error) { return 0, w.err }

func TestMainAndUsageCoverage(t *testing.T) {
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}

	if code := Main(context.Background(), []string{"help"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("Main(help) code = %d", code)
	}
	if !strings.Contains(stdout.String(), "agent-plan init") {
		t.Fatalf("help output = %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := MainNamed(context.Background(), "", []string{}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("MainNamed(no args) code = %d", code)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("stderr = %q", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := MainNamed(context.Background(), "custom-plan", []string{"plan"}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("MainNamed(plan) code = %d", code)
	}
	if !strings.Contains(stderr.String(), "custom-plan plan show") {
		t.Fatalf("stderr = %q", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := MainNamed(context.Background(), "custom-plan", []string{"wat"}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("MainNamed(unknown) code = %d", code)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestCLIHumanCommandsAndErrors(t *testing.T) {
	root := t.TempDir()

	if code := runCLI(context.Background(), "agent-plan", []string{"init", "--dir", filepath.Join(root, "child")}, strings.NewReader(""), io.Discard, io.Discard); code != 0 {
		t.Fatalf("init code = %d", code)
	}

	tests := []struct {
		args []string
		code int
	}{
		{[]string{"add", "--dir", root}, 1},
		{[]string{"claim", "--dir", root}, 1},
		{[]string{"done", "--dir", root}, 1},
		{[]string{"block", "--dir", root}, 1},
	}
	for _, test := range tests {
		if code := runCLI(context.Background(), "agent-plan", test.args, strings.NewReader(""), io.Discard, io.Discard); code != test.code {
			t.Fatalf("runCLI(%v) code = %d, want %d", test.args, code, test.code)
		}
	}

	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	if code := runCLI(context.Background(), "agent-plan", []string{"list", "--dir", root}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("list on missing plan code = %d", code)
	}
	if code := runCLI(context.Background(), "agent-plan", []string{"next", "--dir", root}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("next on missing plan code = %d", code)
	}
	if code := runCLI(context.Background(), "agent-plan", []string{"claim", "--dir", root, "x"}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("claim on missing plan code = %d", code)
	}
	if code := runCLI(context.Background(), "agent-plan", []string{"done", "--dir", root, "x"}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("done on missing plan code = %d", code)
	}
	if code := runCLI(context.Background(), "agent-plan", []string{"block", "--dir", root, "--reason", "x", "x"}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("block on missing plan code = %d", code)
	}
	if code := runCLI(context.Background(), "agent-plan", []string{"plan", "show", "--dir", root}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("plan show on missing plan code = %d", code)
	}

	store, err := Init(root)
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if _, err := store.Add("Task one", AddOptions{Description: "Alpha"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if code := runCLI(context.Background(), "agent-plan", []string{"add", "--dir", root, "--depends-on", "missing", "Task two"}, strings.NewReader(""), io.Discard, io.Discard); code != 1 {
		t.Fatalf("add invalid dep code = %d", code)
	}

	stdout.Reset()
	stderr.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"list", "--dir", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("list code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Task one") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	stdout.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"next", "--dir", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("next code = %d", code)
	}
	if !strings.Contains(stdout.String(), "Task one") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	stdout.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"next", "--dir", root, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("next --json code = %d", code)
	}

	stdout.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"claim", "--dir", root, "001-task-one"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("claim code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "claimed 001-task-one") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	stdout.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"claim", "--dir", root, "--json", "001-task-one"}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("claim duplicate code = %d", code)
	}

	stdout.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"block", "--dir", root, "--reason", "waiting", "001-task-one"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("block code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "blocked 001-task-one") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	stdout.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"plan", "show", "--dir", root}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("plan show code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Plan") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	stdout.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"plan", "show", "--dir", root, "--json"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("plan show --json code = %d", code)
	}

	stdout.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"done", "--dir", root, "--output", "done", "001-task-one"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("done code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "completed 001-task-one") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	stdout.Reset()
	if code := runCLI(context.Background(), "agent-plan", []string{"block", "--dir", root, "--json", "--reason", "x", "001-task-one"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("block --json code = %d", code)
	}
}

func TestCLIHelpers(t *testing.T) {
	if got := splitCSV("a,b"); len(got) != 2 {
		t.Fatalf("splitCSV() = %#v", got)
	}
	if got := splitCSV(" "); got != nil {
		t.Fatalf("splitCSV(blank) = %#v", got)
	}
	if got := commandName(""); got != "agent-plan" {
		t.Fatalf("commandName() = %q", got)
	}
	if got := commandName("custom"); got != "custom" {
		t.Fatalf("commandName(custom) = %q", got)
	}

	summary := TaskSummary{ID: "1", Status: StatusBlocked, Title: "Task", ClaimedBy: "agent", BlockedReason: "wait", DescriptionPreview: "preview"}
	formatted := formatTaskSummary(summary)
	if !strings.Contains(formatted, "claimed=agent") || !strings.Contains(formatted, "reason=wait") {
		t.Fatalf("formatTaskSummary() = %q", formatted)
	}

	if code := writeJSON(failingWriter{err: io.ErrClosedPipe}, map[string]string{"x": "y"}, io.Discard); code != 1 {
		t.Fatalf("writeJSON() code = %d", code)
	}
}

func TestMCPHelpersAndServe(t *testing.T) {
	server := NewMCPServer("/tmp/root")
	if len(server.Tools(context.Background())) == 0 {
		t.Fatal("Tools() returned no tools")
	}
	if got := server.resolveRoot(map[string]any{"root": "/override"}); got != "/override" {
		t.Fatalf("resolveRoot() = %q", got)
	}
	if got := server.resolveRoot(map[string]any{}); got != "/tmp/root" {
		t.Fatalf("resolveRoot(default) = %q", got)
	}
	if !toolErr(errors.New("boom")).IsError {
		t.Fatal("toolErr() should mark result as error")
	}
	if _, err := requiredString(map[string]any{}, "id"); err == nil {
		t.Fatal("requiredString() should fail")
	}
	if got := optionalString(map[string]any{"x": 123}, "x"); got != "123" {
		t.Fatalf("optionalString() = %q", got)
	}
	if got := optionalStringSlice(map[string]any{"x": []string{"a", "b"}}, "x"); len(got) != 2 {
		t.Fatalf("optionalStringSlice([]string) = %#v", got)
	}
	if got := optionalStringSlice(map[string]any{"x": []any{"a", 2}}, "x"); len(got) != 2 || got[1] != "2" {
		t.Fatalf("optionalStringSlice([]any) = %#v", got)
	}
	if got := optionalStringSlice(map[string]any{"x": 1}, "x"); got != nil {
		t.Fatalf("optionalStringSlice(invalid) = %#v", got)
	}
	if got := optionalIntPointer(map[string]any{"x": 7}, "x"); got == nil || *got != 7 {
		t.Fatalf("optionalIntPointer(int) = %#v", got)
	}
	if got := optionalIntPointer(map[string]any{"x": int32(8)}, "x"); got == nil || *got != 8 {
		t.Fatalf("optionalIntPointer(int32) = %#v", got)
	}
	if got := optionalIntPointer(map[string]any{"x": int64(9)}, "x"); got == nil || *got != 9 {
		t.Fatalf("optionalIntPointer(int64) = %#v", got)
	}
	if got := optionalIntPointer(map[string]any{"x": float64(10)}, "x"); got == nil || *got != 10 {
		t.Fatalf("optionalIntPointer(float64) = %#v", got)
	}
	if got := optionalIntPointer(map[string]any{"x": "11"}, "x"); got == nil || *got != 11 {
		t.Fatalf("optionalIntPointer(string) = %#v", got)
	}
	if got := optionalIntPointer(map[string]any{"x": "bad"}, "x"); got != nil {
		t.Fatalf("optionalIntPointer(bad) = %#v", got)
	}
	if got := optionalIntPointer(map[string]any{"x": true}, "x"); got != nil {
		t.Fatalf("optionalIntPointer(bool) = %#v", got)
	}

	request := `{"jsonrpc":"2.0","id":1,"method":"initialize"}`
	framed := "Content-Length: " + strconv.Itoa(len(request)) + "\r\n\r\n" + request
	stdout := bytes.Buffer{}
	stderr := bytes.Buffer{}
	if code := runMCP(context.Background(), strings.NewReader(framed), &stdout, &stderr, []string{"/tmp/root"}); code != 0 {
		t.Fatalf("runMCP() code = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "protocolVersion") {
		t.Fatalf("stdout = %q", stdout.String())
	}

	if err := server.Serve(context.Background(), strings.NewReader(framed), io.Discard); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}
	if code := runMCP(context.Background(), strings.NewReader(framed), failingWriter{err: io.ErrClosedPipe}, &stderr, nil); code != 1 {
		t.Fatalf("runMCP(error) code = %d", code)
	}
}
