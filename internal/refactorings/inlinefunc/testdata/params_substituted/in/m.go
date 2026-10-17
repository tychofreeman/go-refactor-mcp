package m

import "fmt"

func run() {
	n, s := 3, "x"
	report(n*2, s)
}

func report(count int, label string) {
	fmt.Println(label, count)
	fmt.Println(-count)
}
