package m

import (
	"fmt"
	"strconv"
)

func run(s, t string) {
	a, err := strconv.Atoi(s)
	err = second(t, a)
	fmt.Println(err)
}

func second(t string, a int) error {
	var err error
	b, err := strconv.Atoi(t)
	fmt.Println(a + b)
	return err
}
