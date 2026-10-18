package m

import "errors"

func check(n int) error {
	if n < 0 {
		return errors.New("negative")
	}
	return nil
}

func count(ns []int) (int, error) {
	for _, n := range ns {
		if err := check(n); err != nil {
			return -1, err
		}
	}
	return -1, nil
}
