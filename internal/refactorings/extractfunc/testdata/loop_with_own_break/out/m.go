package m

import "fmt"

func run() {
	count()
	fmt.Println("done")
}

func count() {
	for i := 0; i < 3; i++ {
		if i == 2 {
			break
		}
		fmt.Println(i)
	}
}
