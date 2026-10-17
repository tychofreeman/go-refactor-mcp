package m

import "fmt"

func show[T any](v T) {
	{
		type T int
		var n T = 3
		fmt.Println(n)
		fmt.Println(v)
	}
}
