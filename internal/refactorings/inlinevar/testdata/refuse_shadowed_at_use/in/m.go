package m

func F(a int) int {
	b := a * 2
	{
		a := 5
		return a + b
	}
}
