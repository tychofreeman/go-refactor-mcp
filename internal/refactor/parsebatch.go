package refactor

import (
	"encoding/json"
	"fmt"
)

// BatchOf is a batch of steps on a workspace W as the batch tool takes it.
type BatchOf[W Workspace] struct {
	Steps     []StepOf[W]
	Unchecked bool // type-check only at the end ("check": "end")
	Summary   bool // report without the diff ("report": "summary")
}

// ParseBatch reads the batch tool's arguments, finding each step's tool
// with lookup; "check" is a checkpoint. A refusal is returned as text.
func ParseBatch[W Workspace](args json.RawMessage, lookup func(string) (RefactoringOf[W], bool)) (BatchOf[W], string) {
	var a struct {
		Steps []struct {
			ID   string          `json:"id"`
			Tool string          `json:"tool"`
			Args json.RawMessage `json:"args"`
		} `json:"steps"`
		Report string `json:"report"`
		Check  string `json:"check"`
	}
	if err := DecodeArgs(args, &a); err != nil {
		return BatchOf[W]{}, "REFUSED batch: " + err.Error() + "\nbatch takes:\n  steps (required): the steps, in order, each {\"tool\": ..., \"args\": {...}}; a tool's own arguments (file, name, line, ...) go in each step's args\n  report: diff (the default) or summary"
	}
	if a.Check != "" && a.Check != "each" && a.Check != "end" {
		return BatchOf[W]{}, fmt.Sprintf("REFUSED batch: check is %q; give each or end", a.Check)
	}
	if a.Report != "" && a.Report != "diff" && a.Report != "summary" {
		return BatchOf[W]{}, fmt.Sprintf("REFUSED batch: report is %q; give diff or summary", a.Report)
	}
	if len(a.Steps) == 0 {
		return BatchOf[W]{}, "REFUSED batch: steps is empty; give the refactorings and edits to run, in order"
	}
	b := BatchOf[W]{Steps: make([]StepOf[W], len(a.Steps)), Unchecked: a.Check == "end", Summary: a.Report == "summary"}
	for i, st := range a.Steps {
		r, ok := lookup(st.Tool)
		if st.Tool == "check" {
			r, ok = &CheckpointOf[W]{}, true
		}
		if !ok {
			return BatchOf[W]{}, fmt.Sprintf("REFUSED batch: step %d: %q is not a refactoring or edit; a batch runs tools such as rename, add_parameter or replace_statements, and check as a checkpoint, not inspect, undo or batch. Nothing changed.", i+1, st.Tool)
		}
		b.Steps[i] = StepOf[W]{ID: st.ID, Refactoring: r, Args: st.Args}
	}
	return b, ""
}
