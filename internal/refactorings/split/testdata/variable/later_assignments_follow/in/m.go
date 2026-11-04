package m

import "fmt"

func Dig(bed int) {
	depth := bed * 2
	fmt.Println(depth)
	depth = bed + 10
	depth = depth + 1
	fmt.Println(depth)
}
