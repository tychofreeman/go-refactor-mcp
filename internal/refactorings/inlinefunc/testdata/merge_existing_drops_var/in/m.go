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
	err, key = lookup(n)
	fmt.Println(key, err)
	return nil
}

func lookup(n int) (error, string) {
	var key string
	var err error
	key, err = strconv.Itoa(n), nil
	return err, key
}
