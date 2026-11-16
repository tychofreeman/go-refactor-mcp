package m

import "fmt"

func outer(n int) {
	if n > 0 {
		for i := 0; i < n; i++ {
			fmt.Println(fmt.Sprintf(`count:
		%d items`, i) + "!")
		}
	}
}
