package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// serve runs the server over the given lines and returns its replies by id.
func serve(t *testing.T, s *Server, lines ...string) map[string]map[string]any {
	t.Helper()
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Serve(ctx, strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatal(err)
	}
	replies := map[string]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("bad reply %q: %v", line, err)
		}
		id, _ := json.Marshal(m["id"])
		replies[string(id)] = m
	}
	return replies
}

func tools() []Tool {
	return []Tool{
		{Name: "echo", Description: "Echo the text.", Schema: json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`),
			Run: func(ctx context.Context, args json.RawMessage) (string, error) {
				var a struct {
					Text string `json:"text"`
				}
				if err := json.Unmarshal(args, &a); err != nil {
					return "", err
				}
				return a.Text, nil
			}},
		{Name: "fail", Description: "Fail.", Schema: json.RawMessage(`{"type":"object"}`),
			Run: func(context.Context, json.RawMessage) (string, error) { return "", errors.New("no such worker") }},
	}
}

func result(t *testing.T, r map[string]any) map[string]any {
	t.Helper()
	res, ok := r["result"].(map[string]any)
	if !ok {
		t.Fatalf("no result in %v", r)
	}
	return res
}

func text(t *testing.T, r map[string]any) (string, bool) {
	t.Helper()
	res := result(t, r)
	content := res["content"].([]any)
	isErr, _ := res["isError"].(bool)
	return content[0].(map[string]any)["text"].(string), isErr
}

func TestInitializeListAndPing(t *testing.T) {
	r := serve(t, New("contr", "1.2", tools()),
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":"p","method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"initialize"}`,
	)
	if len(r) != 4 {
		t.Errorf("replies = %d, want 4 (no reply to the notification)", len(r))
	}
	init := result(t, r["1"])
	if init["protocolVersion"] != "2025-03-26" || init["serverInfo"].(map[string]any)["version"] != "1.2" {
		t.Errorf("initialize = %v", init)
	}
	if _, ok := init["capabilities"].(map[string]any)["tools"]; !ok {
		t.Errorf("no tools capability: %v", init)
	}
	if result(t, r["3"])["protocolVersion"] != ProtocolVersion {
		t.Errorf("default version = %v", result(t, r["3"]))
	}
	list := result(t, r["2"])["tools"].([]any)
	if len(list) != 2 || list[0].(map[string]any)["name"] != "echo" || list[0].(map[string]any)["inputSchema"] == nil {
		t.Errorf("tools = %v", list)
	}
	if _, ok := r[`"p"`]["result"]; !ok {
		t.Errorf("ping = %v", r[`"p"`])
	}
}

func TestToolCalls(t *testing.T) {
	r := serve(t, New("contr", "1", tools()),
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"fail","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"nope"}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"echo"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":"bad"}`,
		`{"jsonrpc":"2.0","id":6,"method":"resources/list"}`,
		`not json`,
	)
	if got, isErr := text(t, r["1"]); got != "hi" || isErr {
		t.Errorf("echo = %q, %v", got, isErr)
	}
	if got, isErr := text(t, r["2"]); got != "fail: no such worker" || !isErr {
		t.Errorf("fail = %q, %v", got, isErr)
	}
	if got, _ := text(t, r["4"]); got != "" {
		t.Errorf("echo without arguments = %q", got)
	}
	for id, code := range map[string]float64{"3": -32602, "5": -32602, "6": -32601, "null": -32700} {
		e, ok := r[id]["error"].(map[string]any)
		if !ok || e["code"] != code {
			t.Errorf("reply %s = %v, want error %v", id, r[id], code)
		}
	}
}

func TestSlowCallDoesNotBlockOthers(t *testing.T) {
	release := make(chan struct{})
	slow := Tool{Name: "slow", Schema: json.RawMessage(`{}`), Run: func(ctx context.Context, _ json.RawMessage) (string, error) {
		<-release
		return "slow done", nil
	}}
	pr, pw := ioPipe()
	var out syncBuffer
	done := make(chan error, 1)
	go func() { done <- New("c", "1", []Tool{slow, tools()[0]}).Serve(context.Background(), pr, &out) }()
	write := func(s string) { _, _ = pw.Write([]byte(s + "\n")) }
	write(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"slow"}}`)
	write(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"text":"fast"}}}`)
	deadline := time.Now().Add(2 * time.Second)
	for !strings.Contains(out.String(), "fast") {
		if time.Now().After(deadline) {
			t.Fatalf("the fast call waited for the slow one: %q", out.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if strings.Contains(out.String(), "slow done") {
		t.Fatal("slow call ended early")
	}
	close(release)
	_ = pw.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "slow done") {
		t.Errorf("Serve returned before the slow reply: %q", out.String())
	}
}
