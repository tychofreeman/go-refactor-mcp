package m

import (
	"fmt"
	"strconv"
)

func run(s string) {
	var n int
	var err error
	n, err = parse(s)
	fmt.Println(n, err)
}

func parse(s string) (int, error) {
	n, err := strconv.Atoi(s)
	return n, err
}
