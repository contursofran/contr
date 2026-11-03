// Package mcp is a small Model Context Protocol server over stdio: JSON-RPC
// 2.0, one message per line. It serves tools only: initialize, tools/list,
// tools/call and ping. Notifications from the client need no reply and are
// dropped.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"sync"
)

// ProtocolVersion is the newest version this server speaks. It answers a
// client that asks for another known version with that version, and any other
// client with this one.
const ProtocolVersion = "2025-06-18"

var knownVersions = []string{"2024-11-05", "2025-03-26", ProtocolVersion}

// Tool is one tool. Schema is its JSON Schema for the arguments. Run returns
// the text of the result; an error becomes a tool result with isError set, so
// the model sees it.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
	Run         func(ctx context.Context, args json.RawMessage) (string, error)
}

// Server serves a fixed set of tools.
type Server struct {
	Name, Version string
	tools         []Tool
	byName        map[string]Tool
}

// New returns a server with the tools, in that order.
func New(name, version string, tools []Tool) *Server {
	s := &Server{Name: name, Version: version, tools: tools, byName: map[string]Tool{}}
	for _, t := range tools {
		s.byName[t.Name] = t
	}
	return s
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// Serve reads requests from r and writes replies to w until r ends or ctx
// ends. Tool calls run at the same time; replies go out as each one ends.
func (s *Server) Serve(ctx context.Context, r io.Reader, w io.Writer) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var mu sync.Mutex
	enc := json.NewEncoder(w)
	var writeErr error
	reply := func(resp response) {
		mu.Lock()
		defer mu.Unlock()
		if writeErr == nil {
			writeErr = enc.Encode(resp)
		}
	}
	var wg sync.WaitGroup
	defer wg.Wait()

	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	failed := func() error {
		mu.Lock()
		defer mu.Unlock()
		return writeErr
	}
	for sc.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := failed(); err != nil {
			return err
		}
		var req request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			reply(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{-32700, "parse error: " + err.Error()}})
			continue
		}
		if len(req.ID) == 0 || string(req.ID) == "null" {
			continue // a notification
		}
		if req.Method == "tools/call" {
			wg.Add(1)
			go func() {
				defer wg.Done()
				reply(s.call(ctx, req))
			}()
			continue
		}
		reply(s.handle(req))
	}
	if err := failed(); err != nil {
		return err
	}
	return sc.Err()
}

func (s *Server) handle(req request) response {
	resp := response{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p) // no params: answer the default version
		v := p.ProtocolVersion
		if !slices.Contains(knownVersions, v) {
			v = ProtocolVersion
		}
		resp.Result = map[string]any{
			"protocolVersion": v,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		list := make([]map[string]any, 0, len(s.tools))
		for _, t := range s.tools {
			list = append(list, map[string]any{"name": t.Name, "description": t.Description, "inputSchema": t.Schema})
		}
		resp.Result = map[string]any{"tools": list}
	default:
		resp.Error = &rpcError{-32601, "method not found: " + req.Method}
	}
	return resp
}

func (s *Server) call(ctx context.Context, req request) response {
	resp := response{JSONRPC: "2.0", ID: req.ID}
	var p struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		resp.Error = &rpcError{-32602, "bad params: " + err.Error()}
		return resp
	}
	t, ok := s.byName[p.Name]
	if !ok {
		resp.Error = &rpcError{-32602, "unknown tool: " + p.Name}
		return resp
	}
	if len(p.Arguments) == 0 {
		p.Arguments = json.RawMessage("{}")
	}
	text, err := t.Run(ctx, p.Arguments)
	if err != nil {
		resp.Result = toolResult(fmt.Sprintf("%s: %v", p.Name, err), true)
	} else {
		resp.Result = toolResult(text, false)
	}
	return resp
}

func toolResult(text string, isError bool) map[string]any {
	r := map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
	if isError {
		r["isError"] = true
	}
	return r
}
