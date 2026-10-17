package m

import "fmt"

func run(done chan bool) {
	n := 0
	go func() {
		fmt.Println(n)
		done <- true
	}()
	n += 10
	fmt.Println(n)
	<-done
}
