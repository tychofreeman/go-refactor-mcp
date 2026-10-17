package m

import "fmt"

func run(done chan bool) {
	n := 0
	go func() {
		fmt.Println(n)
		done <- true
	}()
	more(&n)
	<-done
}

func more(n *int) {
	*n += 10
	fmt.Println(*n)
}
