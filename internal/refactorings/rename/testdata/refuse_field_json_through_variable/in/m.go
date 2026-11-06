package m

import "encoding/json"

type rec struct {
	Name string
	Age  int
}

func Encode(r rec) ([]byte, error) {
	var x any = r
	return json.Marshal(x)
}
