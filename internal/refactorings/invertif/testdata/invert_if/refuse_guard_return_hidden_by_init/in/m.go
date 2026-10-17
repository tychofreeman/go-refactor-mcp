package m

import (
	"errors"
	"fmt"
)

func step() error { return errors.New("x") }

func run() error {
	var err error
	if err := step(); err == nil {
		fmt.Println("ok")
	}
	return err
}
