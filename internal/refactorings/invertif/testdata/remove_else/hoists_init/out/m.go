package m

import "fmt"

func run(m map[string]int) {
	v, ok := m["k"]
	if !ok {
		return
	}
	fmt.Println(v)
}
