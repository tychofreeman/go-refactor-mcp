package m

import (
	"fmt"

	"example.com/m/refuse"
)

func plan(name string) (string, error) {
	if err := checkName(name); err != nil {
		return "", err
	}
	fmt.Println("name ok")
	return name + "!", nil
}

func checkName(name string) error {
	if name == "" {
		return refuse.Because("empty name")
	}
	if len(name) > 10 {
		return refuse.Becausef("%q is too long", name)
	}
	return nil
}
