package m

import (
	"errors"
	"fmt"
)

func check(name string) error {
	if name == "ok" {
		return nil
	}
	return errors.New("bad name")
}

func plan(name string) (string, error) {
	if name == "" {
		return "", check(name)
	}
	fmt.Println("name ok")
	return name + "!", nil
}
