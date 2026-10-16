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
	var err error
	count, err = checkAndCount(n, count)
	if err != nil {
		return err
	}
	fmt.Println(count)
	return nil
}

func checkAndCount(n int, count int) (int, error) {
	if err := check(n); err != nil {
		return 0, err
	}
	count++
	return count, nil
}
