package m

import "fmt"

func pair() (int, string) { return 1, "a" }

func run() {
	show(pair())
}

func show(n int, s string) {
	fmt.Println(n, s)
}
