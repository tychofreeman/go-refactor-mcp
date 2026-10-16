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

func run(n int) error {
	count := 0
	if err := check(n); err != nil {
		return err
	}
	count++
	fmt.Println(count)
	return nil
}
