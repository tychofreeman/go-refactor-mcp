package m

func f() int {
	println("f")
	return 1
}

func F() int {
	return f() + f()
}
