package m

func load() int  { return 3 }
func other() int { return 4 }

func F(a int) string {
	v := load()
	if a > 0 {
		return "pos"
	} else if v > 0 {
		return "v"
	}
	return ""
}
