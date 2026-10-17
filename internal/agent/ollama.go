package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Message is one chat message, in Ollama's /api/chat format.
type Message struct {
	Role      string     `json:"role"` // system, user, assistant or tool
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	ToolName  string     `json:"tool_name,omitempty"` // on tool results
}

// ToolCall is a function call requested by the model.
type ToolCall struct {
	Function FunctionCall `json:"function"`
}

// FunctionCall names a tool and its arguments. Arguments is normally a
// JSON object; some models send a string holding one, which coerceArgs
// accepts too.
type FunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ToolDef describes a tool to the model.
type ToolDef struct {
	Type     string      `json:"type"` // "function"
	Function FunctionDef `json:"function"`
}

// FunctionDef is a tool's name, description and JSON Schema parameters.
type FunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// Model is a chat model that can call tools.
type Model interface {
	Chat(ctx context.Context, messages []Message, tools []ToolDef) (Message, error)
}

// Ollama is a Model served by Ollama's /api/chat.
type Ollama struct {
	Host   string // e.g. http://localhost:11434
	Model  string // e.g. qwen2.5-coder:14b
	NumCtx int    // context window in tokens; Ollama's default is small
	HTTP   *http.Client
}

type chatRequest struct {
	Model    string         `json:"model"`
	Messages []Message      `json:"messages"`
	Tools    []ToolDef      `json:"tools,omitempty"`
	Stream   bool           `json:"stream"`
	Options  map[string]any `json:"options,omitempty"`
}

type chatResponse struct {
	Message Message `json:"message"`
	Error   string  `json:"error"`
}

// Chat sends the conversation and returns the model's reply.
func (o *Ollama) Chat(ctx context.Context, messages []Message, tools []ToolDef) (Message, error) {
	req := chatRequest{Model: o.Model, Messages: messages, Tools: tools}
	if o.NumCtx > 0 {
		req.Options = map[string]any{"num_ctx": o.NumCtx}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Message{}, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(o.Host, "/")+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return Message{}, err
	}
	hr.Header.Set("Content-Type", "application/json")
	client := o.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(hr)
	if err != nil {
		if ctx.Err() != nil {
			return Message{}, fmt.Errorf("ollama: %w", err)
		}
		return Message{}, fmt.Errorf("ollama: %w (is it running at %s?)", err, o.Host)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return Message{}, fmt.Errorf("ollama: reading response: %w", err)
	}
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return Message{}, fmt.Errorf("ollama: %s: %s", resp.Status, snippet(data))
	}
	if resp.StatusCode != http.StatusOK || cr.Error != "" {
		msg := cr.Error
		if msg == "" {
			msg = snippet(data)
		}
		return Message{}, fmt.Errorf("ollama: %s: %s", resp.Status, msg)
	}
	if cr.Message.Role == "" {
		cr.Message.Role = "assistant"
	}
	return cr.Message, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}
