package m

func first(s string) byte {
	println("first")
	return s[0]
}

func Use(s string) byte { return first(s) }
