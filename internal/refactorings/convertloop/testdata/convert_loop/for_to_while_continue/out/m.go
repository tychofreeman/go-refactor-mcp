package m

import "fmt"

func odd(kinds []string) {
	i := 0
outer:
	for i < 10 {
		if i%3 == 0 {
			i += 2
			continue
		}
		switch kinds[i%len(kinds)] {
		case "skip":
			i += 2
			continue
		}
		for j := 0; j < i; j++ {
			if j == 1 {
				continue
			}
			if j == 2 {
				i += 2
				continue outer
			}
		}
		fmt.Println(i)
		i += 2
	}
}
