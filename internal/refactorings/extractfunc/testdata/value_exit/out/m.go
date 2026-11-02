package m

import "fmt"

var cache = map[string]int{}

func get(k string) (int, error) {
	if done, v, err := step(k); done {
		return v, err
	}
	return 0, fmt.Errorf("missing %s", k)
}

func step(k string) (bool, int, error) {
	if v, ok := cache[k]; ok {
		return true, v, nil
	}
	fmt.Println("miss")
	return false, 0, nil
}
