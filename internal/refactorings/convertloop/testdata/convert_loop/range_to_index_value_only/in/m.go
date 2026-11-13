package m

import "fmt"

func print(names []string) {
	for _, name := range names {
		fmt.Println(name)
	}
}
