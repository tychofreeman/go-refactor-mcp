package m

import (
	"fmt"
	"strconv"
)

func run(s string) {
	n, err := strconv.Atoi(s)
	fmt.Println(n, err)
	n, err = strconv.Atoi(s + "0")
	fmt.Println(n, err)
}
