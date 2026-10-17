package m

import "fmt"

func get() int { return 4 }

func run() {
	fmt.Println(get() * 2)
}
