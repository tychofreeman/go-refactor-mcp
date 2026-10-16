package m

func load() int  { return 3 }
func other() int { return 4 }

func Run(a int) int {
	v := load()
	w := a + 1
	return v + w
}
