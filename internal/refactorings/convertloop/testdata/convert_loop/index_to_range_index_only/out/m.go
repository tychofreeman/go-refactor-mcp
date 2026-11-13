package m

import "fmt"

func count(names []string) {
	for i := range names {
		fmt.Println(i)
	}
}
