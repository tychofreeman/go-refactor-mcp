package m

import "fmt"

func pair() (int, string) { return 1, "a" }

func run() {
	n, s := pair()
	fmt.Println(n, s)
}
