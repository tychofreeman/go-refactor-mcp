package m

import "fmt"

func run() {
	count := 0
	count++
	label := fmt.Sprint("n=", count)
	fmt.Println(label, count)
}
