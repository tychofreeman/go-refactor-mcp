package m

import "fmt"

const base = 10

const limit = base * 2

func run() {
	count()
}

func count() {
	for i := 0; i < limit; i += base {
		fmt.Println(i)
	}
}
