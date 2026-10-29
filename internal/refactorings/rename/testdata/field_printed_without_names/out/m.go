package m

import "fmt"

type rec struct {
	Title string
}

func Show(r rec) string {
	fmt.Println(r)
	return fmt.Sprintf("%v %s %d %[1]q", r, r, 1)
}
