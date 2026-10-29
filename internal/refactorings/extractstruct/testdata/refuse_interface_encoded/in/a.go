package m

import "encoding/json"

type point struct{ X, Y int }

func encode(p point) ([]byte, error) { return json.Marshal(p) }
