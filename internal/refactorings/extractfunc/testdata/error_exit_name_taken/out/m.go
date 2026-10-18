package m

import (
	"errors"
	"fmt"
)

func run(x int) error {
	var err error
	if err := validate(x); err != nil {
		return err
	}
	return err
}

func validate(x int) error {
	if x < 0 {
		return errors.New("negative")
	}
	fmt.Println(x)
	return nil
}
