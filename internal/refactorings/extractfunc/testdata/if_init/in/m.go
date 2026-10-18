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

func run(a, b int) error {
	err := check(a)
	if err != nil {
		return err
	}
	if b > 10 {
		return errors.New("too big")
	}
	fmt.Println("ok")
	return nil
}
