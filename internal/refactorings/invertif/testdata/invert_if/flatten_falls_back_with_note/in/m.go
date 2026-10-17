package m

import "fmt"

func run(m map[string]int) {
	if v, ok := m["k"]; ok {
		fmt.Println(v)
	} else {
		return
	}
}
