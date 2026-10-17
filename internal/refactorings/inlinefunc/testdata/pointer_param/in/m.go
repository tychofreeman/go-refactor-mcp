package m

import "fmt"

func run() {
	total := 0
	add(&total, 3)
	fmt.Println(total)
}

func add(t *int, v int) {
	*t += v
	fmt.Println(*t)
}
