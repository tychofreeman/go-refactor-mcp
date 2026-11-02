package m

var cache = map[string]int{}

func get(k string) (int, error) {
	n := 0
	if v, ok := cache[k]; ok {
		return v, nil
	}
	n = len(k)
	return n, nil
}
