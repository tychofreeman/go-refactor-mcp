package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// sectionLine starts a code section: @@name alone on a line.
var sectionLine = regexp.MustCompile(`(?m)^@@([A-Za-z_][A-Za-z0-9_-]*)[ \t]*$`)

// sectionName is a section's name, as a string "@name" gives it.
var sectionName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)

// withSections returns data, JSON that code sections may follow, with
// every string "@name" of the JSON replaced by the text of section name, as
// written. A section is @@name alone on a line and the lines after it, to
// the next section or the end, its last newline dropped. A section no
// string names is an error, as is one given twice.
func withSections(data []byte) ([]byte, error) {
	starts := sectionLine.FindAllSubmatchIndex(data, -1)
	if len(starts) == 0 {
		return data, nil
	}
	sections := map[string]string{}
	var order []string
	for i, m := range starts {
		name := string(data[m[2]:m[3]])
		end := len(data)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		text := strings.TrimPrefix(string(data[m[1]:end]), "\n")
		text = strings.TrimSuffix(text, "\n")
		if _, ok := sections[name]; ok {
			return nil, fmt.Errorf("section @@%s is given twice", name)
		}
		sections[name] = text
		order = append(order, name)
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(data[:starts[0][0]]))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("the JSON before section @@%s: %v", order[0], err)
	}
	used := map[string]bool{}
	v = replaceSections(v, sections, used)
	for _, name := range order {
		if !used[name] {
			return nil, fmt.Errorf("section @@%s is not used: no string of the JSON is \"@%s\"", name, name)
		}
	}
	for name, ok := range used {
		if _, given := sections[name]; !ok && !given {
			return nil, fmt.Errorf("the JSON names section @@%s, which is not given (did a heredoc end before it?)", name)
		}
	}
	return json.Marshal(v)
}

// replaceSections returns v with each string "@name" that names a section
// replaced by its text, marking the sections it used true and the names
// of sections it does not have false.
func replaceSections(v any, sections map[string]string, used map[string]bool) any {
	switch x := v.(type) {
	case string:
		if name, ok := strings.CutPrefix(x, "@"); ok {
			if text, ok := sections[name]; ok {
				used[name] = true
				return text
			}
			if sectionName.MatchString(name) {
				if _, seen := used[name]; !seen {
					used[name] = false
				}
			}
		}
	case []any:
		for i := range x {
			x[i] = replaceSections(x[i], sections, used)
		}
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(x)) {
			x[k] = replaceSections(x[k], sections, used)
		}
	}
	return v
}
