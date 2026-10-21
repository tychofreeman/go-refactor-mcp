package m

import "fmt"

func show(name string) {
	msg := "hi " + name
	fmt.Println(msg)
}

func f() {
	greeting := "hi " + "bob"
	fmt.Println(greeting)
	x := 1
	fmt.Println(x)
}

func g() {
	m := "hi " + "al"
	fmt.Println(m)
	fmt.Println(m, "again")
}
