package m

import "fmt"

func Water(done chan bool) {
	n := 1
	go func() { fmt.Println(n); done <- true }()
	fmt.Println(n)
	n = 3
	fmt.Println(n)
}
