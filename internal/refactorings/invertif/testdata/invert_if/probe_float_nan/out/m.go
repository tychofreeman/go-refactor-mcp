package main

import (
	"fmt"
	"math"
)

func size(x float64) string {
	if !(x < 1) {
		return "big"
	}
	return "small"
}

func main() {
	fmt.Println(size(0), size(2), size(math.NaN()))
}
