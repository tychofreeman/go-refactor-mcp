package m

import (
	"errors"
	"fmt"
)

func check(n int) error {
	if n < 0 {
		return errors.New("negative")
	}
	return nil
}

func run(n int) {
	var err error
	fmt.Println("checking")
	err = check(n)
	fmt.Println("checked")
	if err != nil {
		fmt.Println(err)
	}
}
