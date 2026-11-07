package refactor

import (
	"encoding/json"
	"fmt"
	"sync"
)

// Retry keeps the last batch refused with nothing changed, so that a
// retry can send only the steps that change.
type Retry struct {
	Kept json.RawMessage // that batch's arguments, or nil
	mu   sync.Mutex
}

// Args returns the batch to run for args: args itself, or, for a retry,
// the kept batch with args' changes. A refusal is returned as text.
func (r *Retry) Args(args json.RawMessage) (json.RawMessage, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var a map[string]json.RawMessage
	if json.Unmarshal(args, &a) != nil || a["retry"] == nil {
		return args, ""
	}
	retry := string(a["retry"]) == "true"
	delete(a, "retry")
	if !retry {
		out, _ := json.Marshal(a)
		return out, ""
	}
	if r.Kept == nil {
		return nil, "REFUSED batch: there is no refused batch to retry (a batch that applies forgets it); send the whole batch, without retry"
	}
	var kept map[string]json.RawMessage
	var steps, changes []map[string]json.RawMessage
	if json.Unmarshal(r.Kept, &kept) != nil || json.Unmarshal(kept["steps"], &steps) != nil || json.Unmarshal(a["steps"], &changes) != nil {
		return nil, `REFUSED batch: a retry's steps are changes, each {"step": N, ...} or {"after": N, ...}`
	}
	added := map[int][]map[string]json.RawMessage{}
	for i, c := range changes {
		step, after := c["step"], c["after"]
		delete(c, "step")
		delete(c, "after")
		var n int
		switch {
		case (step == nil) == (after == nil):
			return nil, fmt.Sprintf(`REFUSED batch: retry change %d needs one of step and after: {"step": N, ...} replaces step N, {"after": N, ...} adds a step after it`, i+1)
		case step != nil:
			if json.Unmarshal(step, &n) != nil || n < 1 || n > len(steps) {
				return nil, fmt.Sprintf("REFUSED batch: retry change %d: step must be from 1 to %d, the refused batch's steps", i+1, len(steps))
			}
			for k, v := range c {
				steps[n-1][k] = v
			}
		default:
			if json.Unmarshal(after, &n) != nil || n < 0 || n > len(steps) {
				return nil, fmt.Sprintf("REFUSED batch: retry change %d: after must be from 0 to %d, the refused batch's steps", i+1, len(steps))
			}
			added[n] = append(added[n], c)
		}
	}
	merged := added[0]
	for i, st := range steps {
		merged = append(merged, st)
		merged = append(merged, added[i+1]...)
	}
	for k, v := range a {
		kept[k] = v
	}
	kept["steps"], _ = json.Marshal(merged)
	out, _ := json.Marshal(kept)
	return out, ""
}

// Keep records how the batch args ended: refused with nothing changed, it
// is kept, and Keep returns the line saying how to retry it; otherwise the
// kept batch is forgotten.
func (r *Retry) Keep(args json.RawMessage, refused bool) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var a struct {
		Steps []json.RawMessage `json:"steps"`
	}
	if !refused || json.Unmarshal(args, &a) != nil || len(a.Steps) == 0 {
		r.Kept = nil
		return ""
	}
	r.Kept = args
	return "\n" + `To retry, send batch with "retry": true and only the steps that change: {"step": N, ...} replaces step N (its tool kept when left out), {"after": N, ...} adds a step after step N (0 for first); numbers are this batch's.` + "\n"
}
