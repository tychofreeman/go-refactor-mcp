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
	if err := checkSize(b); err != nil {
		return err
	}
	fmt.Println("ok")
	return nil
}

func checkSize(b int) error {
	if b > 10 {
		return errors.New("too big")
	}
	return nil
}
