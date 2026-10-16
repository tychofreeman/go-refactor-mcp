package m

import "fmt"

func run() {
outer:
	for i := 0; i < 3; i++ {
		for j := 0; j < 3; j++ {
			if j == i {
				continue outer
			}
			fmt.Println(i, j)
		}
	}
}
