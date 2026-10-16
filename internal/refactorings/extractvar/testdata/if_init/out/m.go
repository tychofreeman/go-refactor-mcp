package m

import "errors"

func validate(n int) error {
	if n < 0 {
		return errors.New("negative")
	}
	return nil
}

func Run(x int) error {
	next := x + 1
	if err := validate(next); err != nil {
		return err
	}
	return nil
}
