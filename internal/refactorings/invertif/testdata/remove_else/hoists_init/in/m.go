package m

import "fmt"

func run(m map[string]int) {
	if v, ok := m["k"]; !ok {
		return
	} else {
		fmt.Println(v)
	}
}
