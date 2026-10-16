package m

func load() int  { return 3 }
func other() int { return 4 }

func F() func() int {
	v := load()
	return func() int { return v }
}
