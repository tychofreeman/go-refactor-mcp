package m

import "fmt"

// loop never returns, so it proves nothing about its result.
func loop(n int) error {
	return loop(n + 1)
}

func plan(name string) (string, error) {
	fmt.Println("planning")
	if name == "" {
		return "", loop(0)
	}
	fmt.Println("name ok")
	return name + "!", nil
}
