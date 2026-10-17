package m

import "fmt"

func load() int { return 1 }

func run() {
	_ = load()
	fmt.Println(2)
}
