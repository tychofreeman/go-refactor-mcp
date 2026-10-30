package refactor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// Schema returns the JSON Schema of the arguments struct T: a property per
// field, named by its json tag and described by its desc tag, in the
// order of the fields. A field whose json tag has omitempty is optional;
// enum lists the allowed strings; schema:"-" leaves a field out, as omit
// leaves out the properties it names (for a tool that shares another's
// struct). The fields of an embedded struct are T's own, as encoding/json
// takes them.
func Schema[T any](omit ...string) json.RawMessage {
	var props bytes.Buffer
	var required []string
	properties(reflect.TypeFor[T](), omit, &props, &required)
	req := ""
	if required != nil {
		b, _ := json.Marshal(required)
		req = fmt.Sprintf(`, "required": %s`, b)
	}
	return json.RawMessage(fmt.Sprintf(`{"type": "object", "properties": {%s}%s, "additionalProperties": false}`, props.String(), req))
}

// properties writes the properties of struct type t to b and appends the
// names it requires to required.
func properties(t reflect.Type, omit []string, b *bytes.Buffer, required *[]string) {
	for f := range t.Fields() {
		if f.Anonymous && f.Type.Kind() == reflect.Struct {
			properties(f.Type, omit, b, required)
			continue
		}
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" || f.Tag.Get("schema") == "-" || slices.Contains(omit, name) {
			continue
		}
		if !strings.Contains(","+opts+",", ",omitempty,") {
			*required = append(*required, name)
		}
		if b.Len() > 0 {
			b.WriteString(", ")
		}
		k, _ := json.Marshal(name)
		d, _ := json.Marshal(f.Tag.Get("desc"))
		fmt.Fprintf(b, `%s: {%s, "description": %s`, k, typeOf(f.Type), d)
		if e := f.Tag.Get("enum"); e != "" {
			v, _ := json.Marshal(strings.Split(e, ","))
			fmt.Fprintf(b, `, "enum": %s`, v)
		}
		b.WriteString("}")
	}
}

// typeOf returns the schema's type entries for a field of type t.
func typeOf(t reflect.Type) string {
	switch t.Kind() {
	case reflect.Pointer:
		return typeOf(t.Elem())
	case reflect.String:
		return `"type": "string"`
	case reflect.Int:
		return `"type": "integer"`
	case reflect.Bool:
		return `"type": "boolean"`
	case reflect.Slice:
		return fmt.Sprintf(`"type": "array", "items": {%s}`, typeOf(t.Elem()))
	case reflect.Map:
		return fmt.Sprintf(`"type": "object", "additionalProperties": {%s}`, typeOf(t.Elem()))
	}
	panic("refactor.Schema: no schema type for " + t.String())
}
