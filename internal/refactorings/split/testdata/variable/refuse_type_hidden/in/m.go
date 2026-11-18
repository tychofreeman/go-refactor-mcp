package m

import "fmt"

type Flag bool

func Rates() {
	var rate Flag = true
	fmt.Println(rate)
	type Flag int
	rate = false
	fmt.Println(rate)
}
