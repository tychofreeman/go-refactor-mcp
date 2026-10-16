package m

import (
	"errors"
	"fmt"
)

func run(x int) error {
	var err error
	if x < 0 {
		return errors.New("negative")
	}
	fmt.Println(x)
	return err
}
