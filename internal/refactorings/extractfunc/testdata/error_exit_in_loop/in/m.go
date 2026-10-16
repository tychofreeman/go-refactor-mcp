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
	for _, x := range xs {
		if err := check(x); err != nil {
			return 0, err
		}
		total += x
	}
	fmt.Println(total)
	return total, nil
}
