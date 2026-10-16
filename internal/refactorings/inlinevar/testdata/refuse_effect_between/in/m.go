package m

import "fmt"

func load() int  { return 3 }
func other() int { return 4 }

func F() int {
	v := load()
	fmt.Println("loaded")
	return v
}
