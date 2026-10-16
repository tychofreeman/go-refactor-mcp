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
	err = verify(n)
	if err != nil {
		fmt.Println(err)
	}
}

func verify(n int) error {
	var err error
	err = check(n)
	fmt.Println("checked")
	return err
}
