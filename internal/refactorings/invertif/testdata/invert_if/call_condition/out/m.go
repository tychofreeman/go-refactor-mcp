package m

import "fmt"

func ready() bool { return true }

func run() {
	if !ready() {
		fmt.Println("else")
	} else {
		fmt.Println("then")
	}
}
