package mcpproto

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string     `json:"jsonrpc"`
	ID      any        `json:"id,omitempty"`
	Result  any        `json:"result,omitempty"`
	Error   *ErrorBody `json:"error,omitempty"`
}

type ErrorBody struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ToolResult struct {
	Content           []TextContent  `json:"content"`
	StructuredContent any            `json:"structuredContent,omitempty"`
	IsError           bool           `json:"isError,omitempty"`
	MetricHints       map[string]int `json:"-"`
}

type ToolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type Provider interface {
	Tools(ctx context.Context) []Tool
	CallTool(ctx context.Context, name string, args map[string]any) ToolResult
}

type Server struct {
	ProtocolVersion string
	Name            string
	Version         string
	Instructions    string
	Provider        Provider
}

type frameReader interface {
	io.Reader
	ReadString(delim byte) (string, error)
}

type frameWriter interface {
	WriteString(string) (int, error)
	Write([]byte) (int, error)
	Flush() error
}

var (
	newFrameReader = func(r io.Reader) frameReader { return bufio.NewReader(r) }
	newFrameWriter = func(w io.Writer) frameWriter { return bufio.NewWriter(w) }
)

func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	reader := newFrameReader(r)
	writer := newFrameWriter(w)

	for {
		payload, err := ReadFrame(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if len(bytes.TrimSpace(payload)) == 0 {
			continue
		}

		var req Request
		if err := json.Unmarshal(payload, &req); err != nil {
			if err := WriteFrame(writer, Response{
				JSONRPC: "2.0",
				Error:   &ErrorBody{Code: -32700, Message: "parse error"},
			}); err != nil {
				return err
			}
			if err := writer.Flush(); err != nil {
				return err
			}
			continue
		}

		resp := s.HandleRequest(ctx, req)
		if resp == nil {
			continue
		}
		if err := WriteFrame(writer, *resp); err != nil {
			return err
		}
		if err := writer.Flush(); err != nil {
			return err
		}
	}
}

func (s *Server) HandleRequest(ctx context.Context, req Request) *Response {
	if req.Method == "" {
		return &Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &ErrorBody{Code: -32600, Message: "invalid request"},
		}
	}

	if strings.HasPrefix(req.Method, "notifications/") && req.ID == nil {
		return nil
	}

	resp := &Response{JSONRPC: "2.0", ID: req.ID}

	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": s.ProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]string{
				"name":    s.Name,
				"version": s.Version,
			},
			"instructions": s.Instructions,
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": s.Provider.Tools(ctx)}
	case "tools/call":
		var params ToolCallParams
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &params); err != nil {
				resp.Error = &ErrorBody{Code: -32602, Message: "invalid tools/call params"}
				return resp
			}
		}
		if params.Arguments == nil {
			params.Arguments = map[string]any{}
		}
		resp.Result = s.Provider.CallTool(ctx, params.Name, params.Arguments)
	default:
		resp.Error = &ErrorBody{Code: -32601, Message: "method not found"}
	}

	return resp
}

func ReadFrame(r frameReader) ([]byte, error) {
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) && line == "" {
				return nil, io.EOF
			}
			if errors.Is(err, io.EOF) {
				payload := strings.TrimRight(line, "\r\n")
				if strings.TrimSpace(payload) == "" {
					return nil, io.EOF
				}
				return []byte(payload), nil
			}
			return nil, err
		}

		return []byte(strings.TrimRight(line, "\r\n")), nil
	}
}

func WriteFrame(w frameWriter, resp Response) error {
	payload, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		return err
	}
	if _, err := w.WriteString("\n"); err != nil {
		return err
	}
	return nil
}
