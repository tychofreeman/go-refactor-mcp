package m

import "errors"

func check(n int) error {
	if n < 0 {
		return errors.New("negative")
	}
	return nil
}

func checkAll(ns []int) error {
	if len(ns) == 0 {
		return errors.New("empty")
	}
	for _, n := range ns {
		if err := check(n); err != nil {
			return err
		}
	}
	return nil
}
