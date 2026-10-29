package m

import "fmt"

type rec struct {
	Title string
}

func Show(r rec, format string) {
	fmt.Printf("%d %#v\n", 1, r)
	fmt.Printf(format, r)
}
