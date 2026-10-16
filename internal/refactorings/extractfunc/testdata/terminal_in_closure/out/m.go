package m

import "fmt"

func run() func(int) string {
	return func(n int) string {
		return show(n)
	}
}

func show(n int) string {
	fmt.Println(n)
	return fmt.Sprint(n)
}
