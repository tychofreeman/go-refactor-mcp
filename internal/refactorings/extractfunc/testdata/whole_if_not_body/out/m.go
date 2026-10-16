package m

import "fmt"

var verbose bool

func run() {
	logVerbose()
	fmt.Println("c")
}

func logVerbose() {
	if verbose {
		fmt.Println("a")
		fmt.Println("b")
	}
}
