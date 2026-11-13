package m

import "fmt"

func count() {
	i := 0
	for i < 9 {
		i++
		if i%3 == 0 {
			continue
		}
		fmt.Println(i)
		i++
	}
}
