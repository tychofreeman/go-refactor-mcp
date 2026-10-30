// Package lang routes the server's tools to the language whose files a
// call names. A Language holds the tools for its source files and the
// workspace they work in; all of them record their changes on one undo
// stack, so undo reverts the last change, whatever its language.
package lang

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/tychofreeman/go-refactor-mcp/internal/history"
)

// Tool is a refactoring or edit a language offers, as the server
// publishes it.
type Tool struct {
	Name        string
	Description string
	Schema      json.RawMessage
}

// Language is the tools for the source files of one language.
type Language interface {
	// Name is the language's name, as refusals give it ("Go").
	Name() string
	// Tools are its refactorings and edits.
	Tools() []Tool
	// Owns reports whether path is a source file of the language.
	Owns(path string) bool
	// Step runs one refactoring or edit; refused reports whether text is
	// a refusal.
	Step(tool string, args json.RawMessage) (text string, refused bool, err error)
	// Batch runs the steps of a batch call.
	Batch(args json.RawMessage) (string, error)
	// Inspect reads code; a refusal is its text.
	Inspect(args json.RawMessage) (string, error)
	// Check reports whether the code compiles, and runs its tests if asked.
	Check(ctx context.Context, args json.RawMessage) (string, error)
	// Undo reverts the change on top of the shared stack, which is this
	// language's.
	Undo() (string, error)
}

// Dispatcher chooses the language for each call.
type Dispatcher struct {
	// Languages are those the server serves; the first takes a call that
	// names no source file of any.
	Languages []Language
	// History is the undo stack every language records its changes on.
	History *history.Stack
}

// For returns the language of the source files args names, as "file",
// "files" or "to_file" at any depth (a batch's steps, inspect's
// targets). When they are of more than one language, it returns a
// refusal saying so instead.
func (d *Dispatcher) For(args json.RawMessage) (Language, string) {
	var v any
	if json.Unmarshal(args, &v) != nil {
		return d.Languages[0], "" // the language's own decoding says what is wrong
	}
	byLang := map[Language][]string{}
	var order []Language
	for _, f := range NamedFiles(v) {
		l := d.owner(f)
		if l == nil {
			continue
		}
		if byLang[l] == nil {
			order = append(order, l)
		}
		byLang[l] = append(byLang[l], f)
	}
	switch len(order) {
	case 0:
		return d.Languages[0], ""
	case 1:
		return order[0], ""
	}
	var parts []string
	for _, l := range order {
		parts = append(parts, fmt.Sprintf("%s (%s)", l.Name(), strings.Join(byLang[l], ", ")))
	}
	return nil, fmt.Sprintf("this call names files of more than one language: %s. A call or batch works on one language; send the steps for each as separate calls.", strings.Join(parts, "; "))
}

// Tools returns the refactorings and edits of every language, each name
// once, in the order the languages and their tools come. A name that
// several languages have takes the properties of each one's schema
// (mergeSchemas), and its description is the first's followed by each
// other's, introduced by the language.
func (d *Dispatcher) Tools() []Tool {
	var out []Tool
	at := map[string]int{}
	for _, l := range d.Languages {
		for _, t := range l.Tools() {
			if i, ok := at[t.Name]; ok {
				out[i].Description += "\n\nIn " + l.Name() + " files: " + t.Description
				out[i].Schema = mergeSchemas(out[i].Schema, t.Schema)
				continue
			}
			at[t.Name] = len(out)
			out = append(out, t)
		}
	}
	return out
}

// mergeSchemas returns the schema a with the properties of b that a
// lacks, requiring only what both require, so that either language's
// arguments fit it; a itself when either is not an object's schema.
func mergeSchemas(a, b json.RawMessage) json.RawMessage {
	var sa, sb map[string]any
	if json.Unmarshal(a, &sa) != nil || json.Unmarshal(b, &sb) != nil {
		return a
	}
	pa, _ := sa["properties"].(map[string]any)
	pb, _ := sb["properties"].(map[string]any)
	if pa == nil || pb == nil {
		return a
	}
	for k, v := range pb {
		if _, ok := pa[k]; !ok {
			pa[k] = v
		}
	}
	both := map[string]bool{}
	rb, _ := sb["required"].([]any)
	for _, r := range rb {
		both[fmt.Sprint(r)] = true
	}
	ra, _ := sa["required"].([]any)
	var required []any
	for _, r := range ra {
		if both[fmt.Sprint(r)] {
			required = append(required, r)
		}
	}
	if required == nil {
		delete(sa, "required")
	} else {
		sa["required"] = required
	}
	out, err := json.Marshal(sa)
	if err != nil {
		return a
	}
	return out
}

// ForUndo returns the language of the change on top of the undo stack,
// or the first language if there is none.
func (d *Dispatcher) ForUndo() Language {
	if c, ok := d.History.Last(); ok {
		var files []string
		for p := range c.After {
			files = append(files, p)
		}
		sort.Strings(files)
		for _, f := range files {
			if l := d.owner(f); l != nil {
				return l
			}
		}
	}
	return d.Languages[0]
}

// owner is the language whose source file path is, or nil.
func (d *Dispatcher) owner(path string) Language {
	for _, l := range d.Languages {
		if l.Owns(path) {
			return l
		}
	}
	return nil
}

// NamedFiles returns the paths v gives as "file", "to_file" or "files",
// at any depth, in the order they appear.
func NamedFiles(v any) []string {
	var out []string
	switch v := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			switch x := v[k].(type) {
			case string:
				if k == "file" || k == "to_file" {
					out = append(out, x)
				}
			case []any:
				if k == "files" {
					for _, f := range x {
						if s, ok := f.(string); ok {
							out = append(out, s)
						}
					}
					continue
				}
				out = append(out, NamedFiles(x)...)
			default:
				out = append(out, NamedFiles(x)...)
			}
		}
	case []any:
		for _, x := range v {
			out = append(out, NamedFiles(x)...)
		}
	}
	return out
}

// MapFiles returns v with each path it gives as "file", "to_file" or
// "files", at any depth, replaced by f of it.
func MapFiles(v any, f func(string) string) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			switch x := x.(type) {
			case string:
				if k == "file" || k == "to_file" {
					out[k] = f(x)
					continue
				}
			case []any:
				if k == "files" {
					var fs []any
					for _, e := range x {
						if s, ok := e.(string); ok {
							fs = append(fs, f(s))
						} else {
							fs = append(fs, e)
						}
					}
					out[k] = fs
					continue
				}
			}
			out[k] = MapFiles(x, f)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = MapFiles(x, f)
		}
		return out
	}
	return v
}
