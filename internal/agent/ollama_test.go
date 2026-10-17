package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOllamaChat(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" || r.Method != http.MethodPost {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(`{"model":"m","message":{"role":"assistant","content":"","tool_calls":[{"function":{"name":"inspect","arguments":{"file":"a.go"}}}]},"done":true}`))
	}))
	defer srv.Close()

	o := &Ollama{Host: srv.URL + "/", Model: "m", NumCtx: 8192}
	tools := []ToolDef{{Type: "function", Function: FunctionDef{Name: "inspect", Description: "d", Parameters: json.RawMessage(`{"type":"object"}`)}}}
	reply, err := o.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, tools)
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "m" || got.Stream || got.Options["num_ctx"] != float64(8192) || len(got.Tools) != 1 || got.Messages[0].Content != "hi" {
		t.Errorf("request = %+v", got)
	}
	if len(reply.ToolCalls) != 1 || reply.ToolCalls[0].Function.Name != "inspect" || string(reply.ToolCalls[0].Function.Arguments) != `{"file":"a.go"}` {
		t.Errorf("reply = %+v", reply)
	}
}

func TestOllamaError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"model \"nope\" not found, try pulling it first"}`))
	}))
	defer srv.Close()
	_, err := (&Ollama{Host: srv.URL, Model: "nope"}).Chat(context.Background(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "try pulling it first") {
		t.Errorf("err = %v", err)
	}
}
