package m

import "fmt"

func ready() bool { return true }

func run() {
	if ready() {
		fmt.Println("then")
	} else {
		fmt.Println("else")
	}
}
