package m

import (
	"errors"
	"fmt"
)

var errSkip = errors.New("skip")

func step1() error { return nil }

func run() error {
	if err := step1(); err != nil && !errors.Is(err, errSkip) {
		return err
	}
	fmt.Println("done")
	return nil
}
