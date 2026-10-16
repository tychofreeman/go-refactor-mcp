package m

import "fmt"

var cache = map[string]int{}

func get(k string) (int, error) {
	if v, ok := cache[k]; ok {
		return v, nil
	}
	fmt.Println("miss")
	return 0, fmt.Errorf("missing %s", k)
}
