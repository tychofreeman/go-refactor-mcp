package m

import "fmt"

func print(names []string) {
	for i := 0; i < len(names); i++ {
		name := names[i]
		fmt.Println(i, name)
	}
}
