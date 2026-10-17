package m

import "fmt"

func run() {
	var r = half(5)
	fmt.Println(r)
}

func half(n int) float64 {
	fmt.Println("halving")
	return float64(n) / 2
}
