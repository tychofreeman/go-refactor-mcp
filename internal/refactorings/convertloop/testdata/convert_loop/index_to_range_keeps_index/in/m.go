package m

import "fmt"

func print(names []string) {
	for i := 0; i < len(names); i++ {
		fmt.Println(i, names[i])
	}
}
