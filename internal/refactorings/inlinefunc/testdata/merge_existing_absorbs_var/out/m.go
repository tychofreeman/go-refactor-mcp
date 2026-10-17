package m

import (
	"fmt"
	"strconv"
)

func run(s string) {
	n, err := strconv.Atoi(s)
	fmt.Println(n, err)
}
