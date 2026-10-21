package m

// weights are the known weights.
var weights = map[string]int{
	"a": 1,
	"b": 2,
}

func weight(s string) int { return weights[s] }
