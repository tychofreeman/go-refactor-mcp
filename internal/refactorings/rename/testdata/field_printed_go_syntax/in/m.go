package m

import "fmt"

type rec struct {
	Name string
}

func Show(r rec, format string) {
	fmt.Printf("%d %#v\n", 1, r)
	fmt.Printf(format, r)
}
