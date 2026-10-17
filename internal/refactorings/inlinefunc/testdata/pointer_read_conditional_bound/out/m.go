package m

import "fmt"

type field struct{ name string }

func run(f *field, verbose bool) {
	s := f.name
	if verbose {
		fmt.Println("name:", s)
	}
}
