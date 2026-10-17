package m

import "fmt"

type field struct{ name string }

func run(f *field, verbose bool) {
	show(f.name, verbose)
}

func show(s string, verbose bool) {
	if verbose {
		fmt.Println("name:", s)
	}
}
