package m

import "fmt"

func count() {
	for i := 0; i < 3; i++ {
		fmt.Println(i)
	}
	for i := 0; i < 2; i++ {
		fmt.Println(-i)
	}
}
