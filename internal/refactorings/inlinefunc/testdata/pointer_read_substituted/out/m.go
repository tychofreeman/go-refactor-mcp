package m

import "fmt"

type field struct{ name string }

func run(f *field) {
	fmt.Println("name:", f.name)
}
