package m

import (
	"errors"
	"fmt"
)

func run(x int) error {
	var err error
	err2 := validate(x)
	if err2 != nil {
		return err2
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
