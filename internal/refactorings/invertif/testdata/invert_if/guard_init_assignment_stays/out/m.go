package m

import "fmt"

func next() int { return 3 }

func run() {
	x := 0
	if x = next(); x <= 0 {
		return
	}
	fmt.Println(x)
}
