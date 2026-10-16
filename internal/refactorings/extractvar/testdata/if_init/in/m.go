package m

import "errors"

func validate(n int) error {
	if n < 0 {
		return errors.New("negative")
	}
	return nil
}

func Run(x int) error {
	if err := validate(x + 1); err != nil {
		return err
	}
	return nil
}
