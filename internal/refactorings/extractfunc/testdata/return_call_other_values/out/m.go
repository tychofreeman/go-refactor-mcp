package m

import "errors"

func check(n int) error {
	if n < 0 {
		return errors.New("negative")
	}
	return nil
}

func count(ns []int) (int, error) {
	if err := checkEach(ns); err != nil {
		return -1, err
	}
	return len(ns), nil
}

func checkEach(ns []int) error {
	for _, n := range ns {
		if err := check(n); err != nil {
			return err
		}
	}
	return nil
}
