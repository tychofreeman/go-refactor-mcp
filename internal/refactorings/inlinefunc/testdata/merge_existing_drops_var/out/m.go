package m

import (
	"fmt"
	"strconv"
)

func run(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	var key string
	key, err = strconv.Itoa(n), nil
	fmt.Println(key, err)
	return nil
}
