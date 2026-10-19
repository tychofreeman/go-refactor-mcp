package refactor

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// checkArgs compares each step's argument names with its tool's input
// schema before any step runs, so that a model that guessed several names
// learns all of them, and what each tool takes, in one refusal.
func checkArgs(steps []Step) error {
	var problems []string
	var tools []Refactoring
	for i, s := range steps {
		problem := argsProblem(s.Refactoring.InputSchema(), s.Args)
		if problem == "" {
			continue
		}
		problems = append(problems, fmt.Sprintf("step %d (%s): %s", i+1, s.Refactoring.Name(), problem))
		if !slices.ContainsFunc(tools, func(r Refactoring) bool { return r.Name() == s.Refactoring.Name() }) {
			tools = append(tools, s.Refactoring)
		}
	}
	if len(problems) == 0 {
		return nil
	}
	var sb strings.Builder
	if len(problems) == 1 {
		sb.WriteString("the arguments of a step are not what its tool takes:")
	} else {
		fmt.Fprintf(&sb, "the arguments of %d steps are not what their tools take:", len(problems))
	}
	for _, p := range problems {
		sb.WriteString("\n  " + p)
	}
	for _, r := range tools {
		sb.WriteString(argsHelp(r.Name(), r.InputSchema()))
	}
	return fmt.Errorf("%s", sb.String())
}

// argsProblem names the arguments args gives that schema does not have,
// and the required ones it leaves out; "" if there are none, or if the
// schema or args cannot be read as objects.
func argsProblem(schema, args json.RawMessage) string {
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
		Required   []string                   `json:"required"`
	}
	if json.Unmarshal(schema, &s) != nil || len(s.Properties) == 0 {
		return ""
	}
	given := map[string]json.RawMessage{}
	if len(args) > 0 && json.Unmarshal(args, &given) != nil {
		return ""
	}
	var unknown, missing []string
	for k := range given {
		if _, ok := s.Properties[k]; !ok {
			unknown = append(unknown, strconv.Quote(k))
		}
	}
	slices.Sort(unknown)
	for _, k := range s.Required {
		if _, ok := given[k]; !ok {
			missing = append(missing, k)
		}
	}
	var parts []string
	if len(unknown) > 0 {
		parts = append(parts, "no argument "+orList(unknown, "or"))
	}
	if len(missing) > 0 {
		parts = append(parts, "missing "+orList(missing, "and"))
	}
	return strings.Join(parts, "; ")
}

// orList joins items as "a, b or c", with conj in place of or.
func orList(items []string, conj string) string {
	if len(items) == 1 {
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " " + conj + " " + items[len(items)-1]
}
