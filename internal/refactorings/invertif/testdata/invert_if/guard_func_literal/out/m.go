package m

import "fmt"

func run() func(bool) {
	return func(ok bool) {
		if !ok {
			return
		}
		fmt.Println("ok")
	}
}
