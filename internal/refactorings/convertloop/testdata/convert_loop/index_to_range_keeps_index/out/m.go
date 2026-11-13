package m

import "fmt"

func print(names []string) {
	for i, name := range names {
		fmt.Println(i, name)
	}
}
