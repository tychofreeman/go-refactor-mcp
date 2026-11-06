package refactor

import "encoding/json"

// OnlyArgs returns raw without the arguments schema's properties lack:
// what a tool that stands for others passes to the one it runs.

func OnlyArgs(raw, schema json.RawMessage) json.RawMessage {
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	given := map[string]json.RawMessage{}
	if json.Unmarshal(schema, &s) != nil || json.Unmarshal(raw, &given) != nil {
		return raw
	}
	for k := range given {
		if _, ok := s.Properties[k]; !ok {
			delete(given, k)
		}
	}
	out, err := json.Marshal(given)
	if err != nil {
		return raw
	}
	return out
}

// withoutOwn returns raw without the arguments the schema own has and the
// schema to lacks: a router's own, which the tool it routes to does not
// take. Arguments neither has stay, for the tool to refuse.
func withoutOwn(raw, own, to json.RawMessage) json.RawMessage {
	var o, t struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	given := map[string]json.RawMessage{}
	if json.Unmarshal(own, &o) != nil || json.Unmarshal(to, &t) != nil || json.Unmarshal(raw, &given) != nil {
		return raw
	}
	dropped := false
	for k := range given {
		_, mine := o.Properties[k]
		if _, theirs := t.Properties[k]; mine && !theirs {
			delete(given, k)
			dropped = true
		}
	}
	if !dropped {
		return raw
	}
	out, err := json.Marshal(given)
	if err != nil {
		return raw
	}
	return out
}
