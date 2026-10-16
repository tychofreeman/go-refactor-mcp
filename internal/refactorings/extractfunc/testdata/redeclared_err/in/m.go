package m

import (
	"fmt"
	"strconv"
)

func run(s, t string) {
	a, err := strconv.Atoi(s)
	b, err := strconv.Atoi(t)
	fmt.Println(a + b)
	fmt.Println(err)
}
