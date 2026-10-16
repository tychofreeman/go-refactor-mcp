package m

import "fmt"

func run() {
	for i := 0; i < 3; i++ {
		if i == 2 {
			break
		}
		fmt.Println(i)
	}
	fmt.Println("done")
}
