package m

import "fmt"

func Dig(bed int) {
	depth := bed * 2
	fmt.Println(depth)
	soil := bed + 10
	soil = soil + 1
	fmt.Println(soil)
}
