package m

import "fmt"

func run() {
outer:
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			fmt.Println(i, j)
			continue outer
		}
	}
}
