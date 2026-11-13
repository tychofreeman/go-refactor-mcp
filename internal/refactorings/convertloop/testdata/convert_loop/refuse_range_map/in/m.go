package m

import "fmt"

func print(ages map[string]int) {
	for name, age := range ages {
		fmt.Println(name, age)
	}
}
