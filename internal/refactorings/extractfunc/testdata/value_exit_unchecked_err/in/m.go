package m

import (
	"errors"
	"fmt"
)

func do() error { return errors.New("x") }

func run(flag bool) error {
	err := do()
	if flag {
		return err
	}
	fmt.Println("x")
	return nil
}
