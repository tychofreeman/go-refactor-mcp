package m

func load() int  { return 3 }
func other() int { return 4 }

func F(a int) bool {
	v := load()
	return a > 0 && v > 0
}
