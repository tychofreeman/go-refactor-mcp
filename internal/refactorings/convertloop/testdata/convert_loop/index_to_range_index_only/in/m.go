package m

import "fmt"

func count(names []string) {
	for i := 0; i < len(names); i++ {
		fmt.Println(i)
	}
}
