package m

import "fmt"

func label(n int) string {
	return fmt.Sprintf(`count:
		%d items`, n)
}

func outer(n int) {
	if n > 0 {
		for i := 0; i < n; i++ {
			fmt.Println(label(i) + "!")
		}
	}
}
