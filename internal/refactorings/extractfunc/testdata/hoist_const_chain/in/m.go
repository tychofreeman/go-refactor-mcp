package m

import "fmt"

func run() {
	const base = 10
	const limit = base * 2
	for i := 0; i < limit; i += base {
		fmt.Println(i)
	}
}
