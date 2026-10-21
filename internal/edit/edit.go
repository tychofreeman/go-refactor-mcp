// Package edit defines text edits on source files and applies them.
//
// Refactorings never mutate ASTs and reprint them (go/printer loses or moves
// comments). Instead they compute byte-range replacements against the
// original source, which keeps every untouched byte exactly as it was.
package edit

import (
	"fmt"
	"sort"
)

// TextEdit replaces the bytes [Start, End) of a file with NewText.
// Start == End is an insertion; NewText == "" is a deletion.
type TextEdit struct {
	Start   int    `json:"start"`
	End     int    `json:"end"`
	NewText string `json:"new_text"`
}

// FileEdit groups the edits for one file. Path is absolute.
type FileEdit struct {
	Path  string     `json:"path"`
	Edits []TextEdit `json:"edits"`
	// Delete deletes the file instead; Edits must then be empty.
	Delete bool `json:"delete,omitempty"`
	// Verbatim writes a file the edits create exactly as they give it,
	// without gofmt (a fixture of unformatted code, a file moved).
	Verbatim bool `json:"verbatim,omitempty"`
}

// Apply returns src with edits applied. Edits may be given in any order.
// Identical duplicate edits are collapsed (the same reference can be found
// through several package variants, e.g. a package and its test variant).
// Overlapping, non-identical edits are an error.
func Apply(src []byte, edits []TextEdit) ([]byte, error) {
	edits = Normalize(edits)
	for i, e := range edits {
		if e.Start < 0 || e.End < e.Start || e.End > len(src) {
			return nil, fmt.Errorf("edit [%d,%d) out of range for %d-byte file", e.Start, e.End, len(src))
		}
		if i > 0 && edits[i-1].End > e.Start {
			return nil, fmt.Errorf("overlapping edits [%d,%d) and [%d,%d)",
				edits[i-1].Start, edits[i-1].End, e.Start, e.End)
		}
	}
	out := make([]byte, 0, len(src))
	last := 0
	for _, e := range edits {
		out = append(out, src[last:e.Start]...)
		out = append(out, e.NewText...)
		last = e.End
	}
	return append(out, src[last:]...), nil
}

// Normalize sorts edits by position and removes exact duplicates.
func Normalize(edits []TextEdit) []TextEdit {
	sorted := append([]TextEdit(nil), edits...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Start != sorted[j].Start {
			return sorted[i].Start < sorted[j].Start
		}
		return sorted[i].End < sorted[j].End
	})
	out := sorted[:0]
	for i, e := range sorted {
		if i > 0 && e == out[len(out)-1] {
			continue
		}
		out = append(out, e)
	}
	return out
}

// Merge combines FileEdits that target the same path, preserving first-seen
// path order.
func Merge(fes []FileEdit) []FileEdit {
	idx := map[string]int{}
	var out []FileEdit
	for _, fe := range fes {
		i, ok := idx[fe.Path]
		if !ok {
			idx[fe.Path] = len(out)
			out = append(out, FileEdit{Path: fe.Path})
			i = len(out) - 1
		}
		out[i].Edits = append(out[i].Edits, fe.Edits...)
		out[i].Delete = out[i].Delete || fe.Delete
		out[i].Verbatim = out[i].Verbatim || fe.Verbatim
	}
	for i := range out {
		out[i].Edits = Normalize(out[i].Edits)
	}
	return out
}
