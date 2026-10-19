package m

import "errors"

func check(s string) error {
	if s == "" {
		return errors.New("empty")
	}
	return nil
}

func validate(s string, err error) error {
	err = check(s)
	return err
}

func Use() error { return validate("a", nil) }
