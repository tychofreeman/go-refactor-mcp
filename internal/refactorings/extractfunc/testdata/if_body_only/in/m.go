package m

import "fmt"

var verbose bool

func run() {
	if verbose {
		fmt.Println("a")
		fmt.Println("b")
	}
}
