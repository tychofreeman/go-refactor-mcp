package m

import "fmt"

func run() {
	f := ratio()
	fmt.Println(f / 2)
}

func ratio() float64 {
	fmt.Println("computing")
	return 3
}
