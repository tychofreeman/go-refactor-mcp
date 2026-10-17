package m

import (
	"fmt"

	"example.com/m/refuse"
)

func plan(name string) (string, error) {
	if name == "" {
		return "", refuse.Because("empty name")
	}
	if len(name) > 10 {
		return "", refuse.Becausef("%q is too long", name)
	}
	fmt.Println("name ok")
	return name + "!", nil
}
