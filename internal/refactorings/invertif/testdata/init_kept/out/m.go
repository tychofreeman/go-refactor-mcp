package m

import "fmt"

func run(m map[string]int) {
	if v, ok := m["k"]; !ok {
		fmt.Println("missing")
	} else {
		fmt.Println(v)
	}
}
