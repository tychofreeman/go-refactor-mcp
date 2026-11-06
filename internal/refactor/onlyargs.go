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
