package m

func f() int { return 1 }

func F() int {
	return f() + f()
}
