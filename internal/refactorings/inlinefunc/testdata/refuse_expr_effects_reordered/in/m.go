package m

import "fmt"

func a() int { return 1 }
func b() int { return 2 }

func run() {
	fmt.Println(sub(a(), b()))
}

func sub(x, y int) int {
	return y - x
}
