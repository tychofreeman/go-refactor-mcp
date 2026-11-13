package m

import "fmt"

func count() {
	for i := 0; i < 9; {
		i++
		if i%3 == 0 {
			continue
		}
		fmt.Println(i)
		i++
	}
}
