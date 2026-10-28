package mcpserver

import (
	"encoding/json"
	"strings"
)

// LanguageFor returns the name of the language whose files args names
// ("Go" or "TypeScript"), or "" when they name files of several.
func (s *Server) LanguageFor(args json.RawMessage) string {
	l, refused := s.langs.For(args)
	if refused != "" || l == nil {
		return ""
	}
	return l.Name()
}

// Call runs the tool (inspect, batch, or a refactoring or edit) with args
// in the language of the files they name, as a tool call does: its text,
// and whether it was refused.
func (s *Server) Call(tool string, args json.RawMessage) (string, bool, error) {
	l, refused := s.langs.For(args)
	if refused != "" {
		return refused, true, nil
	}
	switch tool {
	case "inspect":
		text, err := l.Inspect(args)
		return text, false, err
	case "batch":
		text, err := l.Batch(args)
		return text, strings.HasPrefix(text, "REFUSED"), err
	}
	return l.Step(tool, args)
}
