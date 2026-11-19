package b

import "example.com/m/a"

// All are the shipments.
var All = []a.S{{ID: "2", Road: "nw-0042"}, {Rail: "Y9"}, {Road: "se-1111", Rail: "Y7"}}

// Codes is keyed by a variable named Road.
func Codes(Road string) map[string]int { return map[string]int{Road: 1} }
