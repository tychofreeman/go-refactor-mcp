package m

import "encoding/json"

type rec struct {
	Name   string
	secret int
}

func Encode(r rec) ([]byte, error) { r.secret++; return json.Marshal(r) }
