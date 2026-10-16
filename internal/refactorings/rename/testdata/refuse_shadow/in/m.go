package m

func F() int {
	a := 1
	{
		b := 2
		return a + b
	}
}
