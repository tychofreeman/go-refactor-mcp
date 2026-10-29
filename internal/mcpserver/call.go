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

// LoadUndo replaces the undo stack, which every language shares, with
// the one SaveUndo wrote to path; a missing file is an empty stack.
func (s *Server) LoadUndo(path string) error { return s.langs.History.Load(path) }

// SaveUndo writes the undo stack to path.
func (s *Server) SaveUndo(path string) error { return s.langs.History.Save(path) }

// Undo reverts the last change, in the language whose files it changed,
// as the undo tool does.
func (s *Server) Undo() (string, error) { return s.langs.ForUndo().Undo() }

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
