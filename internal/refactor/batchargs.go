package refactor

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// checkArgs compares each step's argument names with its tool's input
// schema before any step runs, so that a model that guessed several names
// learns all of them, and what each tool takes, in one refusal.
func checkArgs[W Workspace](steps []StepOf[W]) error {
	var problems []string
	var tools []RefactoringOf[W]
	for i, s := range steps {
		problem := argsProblem(s.Refactoring.InputSchema(), s.Args)
		if problem == "" {
			continue
		}
		problems = append(problems, fmt.Sprintf("step %d (%s): %s", i+1, s.Refactoring.Name(), problem))
		if !slices.ContainsFunc(tools, func(r RefactoringOf[W]) bool { return r.Name() == s.Refactoring.Name() }) {
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
		sb.WriteString(ArgsHelp(r.Name(), r.InputSchema()))
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
	args, _ = aliasArgs(schema, args)
	given := map[string]json.RawMessage{}
	if len(args) > 0 && json.Unmarshal(args, &given) != nil {
		return ""
	}
	for k, v := range given {
		if base, ok := fileArgOf(s.Properties, given, k); ok {
			given[base] = v
			delete(given, k)
		}
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

// contentAliases are names a model gives content (the new code) by.
var contentAliases = []string{"new_text", "new_code", "new_content", "code", "body", "statements", "source"}

// aliasArgs renames each argument of args that schema does not have to
// the one it can only mean: an argument schema has and args does not
// give, whose name is a word of the given name (old_text: old), or else,
// for content, one of contentAliases. It returns args unchanged if there
// are none, and a note on the names it took.
func aliasArgs(schema, args json.RawMessage) (json.RawMessage, string) {
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	given := map[string]json.RawMessage{}
	if json.Unmarshal(schema, &s) != nil || len(s.Properties) == 0 || len(args) == 0 || json.Unmarshal(args, &given) != nil {
		return args, ""
	}
	free := func(p string) bool {
		_, has := s.Properties[p]
		_, taken := given[p]
		return has && !taken
	}
	var took []string
	for _, k := range slices.Sorted(maps.Keys(given)) {
		if _, ok := s.Properties[k]; ok {
			continue
		}
		if _, ok := fileArgOf(s.Properties, given, k); ok {
			continue // read by readFileArgs
		}
		var fits []string
		for _, w := range strings.Split(k, "_") {
			if free(w) && !slices.Contains(fits, w) {
				fits = append(fits, w)
			}
		}
		if len(fits) == 0 && slices.Contains(contentAliases, k) && free("content") {
			fits = []string{"content"}
		}
		if len(fits) != 1 {
			continue
		}
		given[fits[0]] = given[k]
		delete(given, k)
		took = append(took, fmt.Sprintf("%q as %s", k, fits[0]))
	}
	if len(took) == 0 {
		return args, ""
	}
	out, err := json.Marshal(given)
	if err != nil {
		return args, ""
	}
	return out, "took " + orList(took, "and")
}

// fileArgOf returns the argument the given argument k stands for when it
// names a file to read that argument from: k is base_file, base is an
// argument of the schema's properties that given does not give, and k is
// not itself one (create_from_usage's to_file).
func fileArgOf(props map[string]json.RawMessage, given map[string]json.RawMessage, k string) (string, bool) {
	base, ok := strings.CutSuffix(k, "_file")
	if !ok {
		return "", false
	}
	_, own := props[k]
	_, has := props[base]
	_, taken := given[base]
	return base, !own && has && !taken
}

// readFileArgs replaces each argument of args that names a file to read
// another from (content_file for content) with that file's text, read
// from a path absolute or relative to dir.
func readFileArgs(dir string, schema, args json.RawMessage) (json.RawMessage, error) {
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	given := map[string]json.RawMessage{}
	if json.Unmarshal(schema, &s) != nil || len(s.Properties) == 0 || len(args) == 0 || json.Unmarshal(args, &given) != nil {
		return args, nil
	}
	changed := false
	for _, k := range slices.Sorted(maps.Keys(given)) {
		base, ok := fileArgOf(s.Properties, given, k)
		if !ok {
			continue
		}
		var path string
		if err := json.Unmarshal(given[k], &path); err != nil {
			return nil, Refusef("%s must be a path, a string", k)
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return nil, Refusef("cannot read %s %s: %v", k, path, err)
		}
		given[base], _ = json.Marshal(string(text))
		delete(given, k)
		changed = true
	}
	if !changed {
		return args, nil
	}
	return json.Marshal(given)
}

// orList joins items as "a, b or c", with conj in place of or.
func orList(items []string, conj string) string {
	if len(items) == 1 {
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " " + conj + " " + items[len(items)-1]
}
