package m

import "fmt"

var verbose bool

func run() {
	if verbose {
		logAB()
	}
}

func logAB() {
	fmt.Println("a")
	fmt.Println("b")
}
