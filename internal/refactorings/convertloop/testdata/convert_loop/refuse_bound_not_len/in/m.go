package m

import "fmt"

func print(names []string, n int) {
	for i := 0; i < n; i++ {
		fmt.Println(names[i])
	}
}
