package m

func load() int  { return 3 }
func other() int { return 4 }

func Twice() int {
	return load() * 2
}
