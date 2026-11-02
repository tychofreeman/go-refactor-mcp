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
	if done, v, err := checkName(name); done {
		return v, err
	}
	return name + "!", nil
}

func checkName(name string) (bool, string, error) {
	if name == "" {
		return true, "", check(name)
	}
	fmt.Println("name ok")
	return false, "", nil
}
