package m

import "fmt"

type field struct{ name string }

func run(f *field) {
	show(f.name)
}

func show(s string) {
	fmt.Println("name:", s)
}
