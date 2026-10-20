package m

import "encoding/json"

type rec struct {
	Name string
	Age  int
}

func Encode(r rec) ([]byte, error) { return json.Marshal(r) }
