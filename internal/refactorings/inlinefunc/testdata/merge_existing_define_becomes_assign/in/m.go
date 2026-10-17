package m

import (
	"fmt"
	"strconv"
)

func run(s string) {
	n, err := strconv.Atoi(s)
	fmt.Println(n, err)
	n, err = again(s)
	fmt.Println(n, err)
}

func again(s string) (int, error) {
	m, e := strconv.Atoi(s + "0")
	return m, e
}
