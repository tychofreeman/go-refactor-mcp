package m

import "fmt"

func run() {
	total := 0
	for i := 0; i < 10; i++ {
		total += i
	}
	fmt.Println(total)
	fmt.Println("done")
}
