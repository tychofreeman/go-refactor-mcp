package m

import "fmt"

func run() {
	logged("hi")
}

func logged(s string) bool {
	fmt.Println("logging")
	return fmt.Sprint(s) == "" && len(s) > 0 || count(s) > 0
}

func count(s string) int { return len(s) }
