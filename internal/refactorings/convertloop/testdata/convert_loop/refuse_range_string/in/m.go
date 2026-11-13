package m

import "fmt"

func print(s string) {
	for _, r := range s {
		fmt.Println(r)
	}
}
