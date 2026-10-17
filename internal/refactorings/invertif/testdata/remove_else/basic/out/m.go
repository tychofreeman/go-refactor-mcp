package m

import (
	"errors"
	"fmt"
)

func run(err error) error {
	if err != nil {
		return errors.New("failed")
	}
	fmt.Println("ok")
	fmt.Println("still ok")
	return nil
}
