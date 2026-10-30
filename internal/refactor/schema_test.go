package refactor

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

type shared struct {
	File string `json:"file" desc:"Path of the file."`
}

type sample struct {
	shared
	Line   int               `json:"line" desc:"1-based line."`
	Column int               `json:"column,omitempty" desc:"Optional column."`
	Names  []string          `json:"names,omitempty" desc:"Some names."`
	Where  string            `json:"where,omitempty" desc:"Where." enum:"before,after"`
	Keep   *bool             `json:"keep,omitempty" desc:"Keep it."`
	Values map[string]string `json:"values,omitempty" desc:"By name."`
	Wrap   bool              `json:"wrap,omitempty" schema:"-"`
}

func TestSchema(t *testing.T) {
	want := `{"type": "object", "properties": {
	  "file": {"type": "string", "description": "Path of the file."},
	  "line": {"type": "integer", "description": "1-based line."},
	  "column": {"type": "integer", "description": "Optional column."},
	  "names": {"type": "array", "items": {"type": "string"}, "description": "Some names."},
	  "where": {"type": "string", "description": "Where.", "enum": ["before", "after"]},
	  "keep": {"type": "boolean", "description": "Keep it."},
	  "values": {"type": "object", "additionalProperties": {"type": "string"}, "description": "By name."}
	}, "required": ["file", "line"], "additionalProperties": false}`
	var got, exp any
	s := Schema[sample]()
	if err := json.Unmarshal(s, &got); err != nil {
		t.Fatalf("%v: %s", err, s)
	}
	json.Unmarshal([]byte(want), &exp)
	if !reflect.DeepEqual(got, exp) {
		t.Errorf("got %s", s)
	}
	if order := propertyOrder(t, s); !reflect.DeepEqual(order, []string{"file", "line", "column", "names", "where", "keep", "values"}) {
		t.Errorf("properties in order %v", order)
	}
	if order := propertyOrder(t, Schema[sample]("names", "keep")); !reflect.DeepEqual(order, []string{"file", "line", "column", "where", "values"}) {
		t.Errorf("with names and keep omitted, properties %v", order)
	}
}

// propertyOrder returns the names of the schema's properties as written.
func propertyOrder(t *testing.T, s json.RawMessage) []string {
	var top struct {
		Properties json.RawMessage `json:"properties"`
	}
	json.Unmarshal(s, &top)
	dec := json.NewDecoder(bytes.NewReader(top.Properties))
	dec.Token()
	var out []string
	for dec.More() {
		k, _ := dec.Token()
		out = append(out, k.(string))
		var skip json.RawMessage
		dec.Decode(&skip)
	}
	return out
}

func TestNamed(t *testing.T) {
	n := Called[sample]("water", "keep")
	if n.Name() != "water" {
		t.Errorf("Name() = %q", n.Name())
	}
	if got, want := string(n.InputSchema()), string(Schema[sample]("keep")); got != want {
		t.Errorf("InputSchema() = %s, want %s", got, want)
	}
}
