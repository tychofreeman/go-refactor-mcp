package agent

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// coerceArgs turns a model's tool arguments into the JSON object the tool
// expects, fixing only what the tool's schema makes unambiguous: arguments
// sent as a string holding a JSON object, a missing or null object, and
// numbers or booleans sent as strings ("12" for an integer property). The
// tools still decode strictly, so anything else (a misspelled or unknown
// field, a wrong type that cannot be converted) reaches them unchanged and
// is refused with a message the model can act on.
func coerceArgs(raw json.RawMessage, schema json.RawMessage) (json.RawMessage, error) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return json.RawMessage("{}"), nil
	}
	if raw[0] == '"' { // a string holding the object
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, fmt.Errorf("arguments are not valid JSON: %v", err)
		}
		if strings.TrimSpace(s) == "" {
			return json.RawMessage("{}"), nil
		}
		raw = json.RawMessage(s)
	}
	var args map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&args); err != nil {
		return nil, fmt.Errorf("arguments must be a JSON object: %v", err)
	}
	var sch struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	_ = json.Unmarshal(schema, &sch) // no schema: no coercion
	for name, v := range args {
		s, ok := v.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		switch sch.Properties[name].Type {
		case "integer":
			if n, err := strconv.Atoi(s); err == nil {
				args[name] = n
			}
		case "number":
			if f, err := strconv.ParseFloat(s, 64); err == nil {
				args[name] = f
			}
		case "boolean":
			if b, err := strconv.ParseBool(s); err == nil {
				args[name] = b
			}
		}
	}
	out, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	return out, nil
}
