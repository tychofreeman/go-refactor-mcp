package m

import "encoding/json"

type rec struct {
	Name   string
	hidden int
}

func Encode(r rec) ([]byte, error) { r.hidden++; return json.Marshal(r) }
