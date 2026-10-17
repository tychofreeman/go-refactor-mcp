package m

import "fmt"

func run(n int, ok bool) {
	switch n {
	case 1:
		if ok {
			fmt.Println("ok")
		}
	}
}
