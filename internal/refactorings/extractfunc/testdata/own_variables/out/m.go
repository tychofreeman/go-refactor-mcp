package m

import "fmt"

func run() {
	printTotal()
	fmt.Println("done")
}

func printTotal() {
	total := 0
	for i := 0; i < 10; i++ {
		total += i
	}
	fmt.Println(total)
}
