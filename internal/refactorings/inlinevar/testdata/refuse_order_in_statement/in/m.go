package m

func load() int  { return 3 }
func other() int { return 4 }

func F() int {
	v := load()
	return other() + v
}
