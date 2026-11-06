package m

import "encoding/json"

type rec struct {
	Name string
	Age  int
}

func box(r rec) any { return r }

func Encode(r rec) ([]byte, error) { return json.Marshal(box(r)) }
