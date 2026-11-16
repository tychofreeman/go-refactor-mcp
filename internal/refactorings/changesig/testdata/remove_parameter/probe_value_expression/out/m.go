package main

import "fmt"

func price(qty, unit int) int {
	return (qty * unit) + 1
}

func main() {
	q, u := 2, 5
	fmt.Println(price(2, 3), price(q, u))
}
