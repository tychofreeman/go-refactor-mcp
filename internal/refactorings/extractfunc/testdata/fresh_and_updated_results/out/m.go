package m

import "fmt"

func run() {
	count := 0
	var label string
	count, label = describe(count)
	fmt.Println(label, count)
}

func describe(count int) (int, string) {
	count++
	label := fmt.Sprint("n=", count)
	return count, label
}
