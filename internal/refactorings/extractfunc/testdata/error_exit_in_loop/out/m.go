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

func sum(xs []int) (int, error) {
	total := 0
	var err error
	total, err = add(xs, total)
	if err != nil {
		return 0, err
	}
	fmt.Println(total)
	return total, nil
}

func add(xs []int, total int) (int, error) {
	for _, x := range xs {
		if err := check(x); err != nil {
			return 0, err
		}
		total += x
	}
	return total, nil
}
