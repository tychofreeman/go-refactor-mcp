package main

import "fmt"

func price(qty, unit, total int) int {
	return total + 1
}

func main() {
	q, u := 2, 5
	fmt.Println(price(2, 3, 2*3), price(q, u, q*u))
}
