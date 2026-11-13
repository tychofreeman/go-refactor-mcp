package m

import "fmt"

func odd(kinds []string) {
outer:
	for i := 0; i < 10; i += 2 {
		if i%3 == 0 {
			continue
		}
		switch kinds[i%len(kinds)] {
		case "skip":
			continue
		}
		for j := 0; j < i; j++ {
			if j == 1 {
				continue
			}
			if j == 2 {
				continue outer
			}
		}
		fmt.Println(i)
	}
}
