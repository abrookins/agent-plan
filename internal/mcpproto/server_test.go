package mcpproto

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

type testProvider struct{}

func (testProvider) Tools(context.Context) []Tool {
	return []Tool{{Name: "demo"}}
}

func (testProvider) CallTool(_ context.Context, name string, args map[string]any) ToolResult {
	return ToolResult{
		Content:           []TextContent{{Type: "text", Text: name}},
		StructuredContent: args,
	}
}

type errWriter struct{ err error }

func (w errWriter) Write(_ []byte) (int, error) { return 0, w.err }

type payloadErrFrameWriter struct{}

func (payloadErrFrameWriter) WriteString(string) (int, error) { return 1, nil }
func (payloadErrFrameWriter) Write([]byte) (int, error)       { return 0, io.ErrClosedPipe }
func (payloadErrFrameWriter) Flush() error                    { return nil }

type newlineErrFrameWriter struct{}

func (newlineErrFrameWriter) Write(p []byte) (int, error)     { return len(p), nil }
func (newlineErrFrameWriter) WriteString(string) (int, error) { return 0, io.ErrClosedPipe }
func (newlineErrFrameWriter) Flush() error                    { return nil }

func frame(body string) string {
	return body + "\n"
}

func TestHandleRequestBranches(t *testing.T) {
	server := &Server{
		ProtocolVersion: "1",
		Name:            "agent-plan",
		Version:         "test",
		Instructions:    "demo",
		Provider:        testProvider{},
	}

	if resp := server.HandleRequest(context.Background(), Request{}); resp == nil || resp.Error == nil || resp.Error.Code != -32600 {
		t.Fatalf("invalid request response = %#v", resp)
	}

	if resp := server.HandleRequest(context.Background(), Request{Method: "notifications/demo"}); resp != nil {
		t.Fatalf("notification response = %#v, want nil", resp)
	}

	resp := server.HandleRequest(context.Background(), Request{ID: 1, Method: "initialize"})
	if resp == nil || resp.Error != nil {
		t.Fatalf("initialize response = %#v", resp)
	}

	resp = server.HandleRequest(context.Background(), Request{ID: 2, Method: "ping"})
	if resp == nil || resp.Error != nil {
		t.Fatalf("ping response = %#v", resp)
	}

	resp = server.HandleRequest(context.Background(), Request{ID: 3, Method: "tools/list"})
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/list response = %#v", resp)
	}

	resp = server.HandleRequest(context.Background(), Request{
		ID:     4,
		Method: "tools/call",
		Params: json.RawMessage(`{"name":"demo","arguments":{"x":1}}`),
	})
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/call response = %#v", resp)
	}

	resp = server.HandleRequest(context.Background(), Request{
		ID:     5,
		Method: "tools/call",
		Params: json.RawMessage(`{"name":`),
	})
	if resp == nil || resp.Error == nil || resp.Error.Code != -32602 {
		t.Fatalf("invalid tools/call response = %#v", resp)
	}

	resp = server.HandleRequest(context.Background(), Request{ID: 55, Method: "tools/call"})
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/call empty response = %#v", resp)
	}

	resp = server.HandleRequest(context.Background(), Request{ID: 6, Method: "unknown"})
	if resp == nil || resp.Error == nil || resp.Error.Code != -32601 {
		t.Fatalf("method not found response = %#v", resp)
	}
}

func TestReadFrameAndWriteFrame(t *testing.T) {
	body := `{"jsonrpc":"2.0","method":"ping"}`
	payload, err := ReadFrame(bufio.NewReader(strings.NewReader(frame(body))))
	if err != nil || string(payload) != body {
		t.Fatalf("ReadFrame() payload=%q err=%v", string(payload), err)
	}

	payload, err = ReadFrame(bufio.NewReader(strings.NewReader(body)))
	if err != nil || string(payload) != body {
		t.Fatalf("ReadFrame(final line) payload=%q err=%v", string(payload), err)
	}
	if _, err := ReadFrame(bufio.NewReader(strings.NewReader(""))); !errors.Is(err, io.EOF) {
		t.Fatalf("ReadFrame() err = %v, want EOF", err)
	}

	buffer := bytes.Buffer{}
	writer := bufio.NewWriter(&buffer)
	if err := WriteFrame(writer, Response{JSONRPC: "2.0", Result: map[string]string{"ok": "1"}}); err != nil {
		t.Fatalf("WriteFrame() error = %v", err)
	}
	if err := writer.Flush(); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if strings.Contains(buffer.String(), "Content-Length:") || !strings.HasSuffix(buffer.String(), "\n") {
		t.Fatalf("buffer = %q", buffer.String())
	}
	var resp Response
	if err := json.Unmarshal([]byte(strings.TrimSpace(buffer.String())), &resp); err != nil {
		t.Fatalf("json.Unmarshal(written response) error = %v", err)
	}

	if err := WriteFrame(bufio.NewWriterSize(errWriter{err: io.ErrClosedPipe}, 1), Response{JSONRPC: "2.0", Result: strings.Repeat("x", 32)}); err == nil {
		t.Fatal("WriteFrame() should fail when writer fails")
	}
	if err := WriteFrame(payloadErrFrameWriter{}, Response{JSONRPC: "2.0", Result: map[string]string{"ok": "1"}}); err == nil {
		t.Fatal("WriteFrame() should fail when payload write fails")
	}
	if err := WriteFrame(newlineErrFrameWriter{}, Response{JSONRPC: "2.0", Result: map[string]string{"ok": "1"}}); err == nil {
		t.Fatal("WriteFrame() should fail when newline write fails")
	}
	if err := WriteFrame(bufio.NewWriter(io.Discard), Response{JSONRPC: "2.0", Result: func() {}}); err == nil {
		t.Fatal("WriteFrame() should fail on unsupported JSON payload")
	}
}

func TestServe(t *testing.T) {
	server := &Server{
		ProtocolVersion: "1",
		Name:            "agent-plan",
		Version:         "test",
		Instructions:    "demo",
		Provider:        testProvider{},
	}

	input := frame("{") + frame(`{"jsonrpc":"2.0","id":1,"method":"ping"}`) + frame(`{"jsonrpc":"2.0","method":"notifications/demo"}`)
	output := bytes.Buffer{}
	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatalf("Serve() error = %v", err)
	}

	reader := bufio.NewReader(&output)
	first, err := ReadFrame(reader)
	if err != nil {
		t.Fatalf("ReadFrame(first) error = %v", err)
	}
	var firstResp Response
	if err := json.Unmarshal(first, &firstResp); err != nil {
		t.Fatalf("json.Unmarshal(first) error = %v", err)
	}
	if firstResp.Error == nil || firstResp.Error.Code != -32700 {
		t.Fatalf("first response = %#v", firstResp)
	}

	second, err := ReadFrame(reader)
	if err != nil {
		t.Fatalf("ReadFrame(second) error = %v", err)
	}
	var secondResp Response
	if err := json.Unmarshal(second, &secondResp); err != nil {
		t.Fatalf("json.Unmarshal(second) error = %v", err)
	}
	if secondResp.Error != nil {
		t.Fatalf("second response = %#v", secondResp)
	}
	if _, err := ReadFrame(reader); !errors.Is(err, io.EOF) {
		t.Fatalf("ReadFrame(third) err = %v, want EOF", err)
	}

	if err := server.Serve(context.Background(), strings.NewReader(frame("{")), errWriter{err: io.ErrClosedPipe}); err == nil {
		t.Fatal("Serve() should fail when parse-error flush fails")
	}
	if err := server.Serve(context.Background(), strings.NewReader(frame(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)), errWriter{err: io.ErrClosedPipe}); err == nil {
		t.Fatal("Serve() should fail when output writer fails")
	}

	oldNewFrameWriter := newFrameWriter
	t.Cleanup(func() {
		newFrameWriter = oldNewFrameWriter
	})
	newFrameWriter = func(w io.Writer) frameWriter {
		return bufio.NewWriterSize(errWriter{err: io.ErrClosedPipe}, 1)
	}
	if err := server.Serve(context.Background(), strings.NewReader(frame("{")), io.Discard); err == nil {
		t.Fatal("Serve() should fail when parse-error WriteFrame fails")
	}
	if err := server.Serve(context.Background(), strings.NewReader(frame(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)), io.Discard); err == nil {
		t.Fatal("Serve() should fail when response WriteFrame fails")
	}
	newFrameWriter = oldNewFrameWriter

	if err := server.Serve(context.Background(), strings.NewReader("\n"+frame(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)), io.Discard); err != nil {
		t.Fatalf("Serve(empty line) error = %v", err)
	}
}
