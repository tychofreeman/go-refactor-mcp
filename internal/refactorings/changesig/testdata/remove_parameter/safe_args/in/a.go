package m

type cfg struct{ dir string }

func f(n int, x string) int { return n }

func Use(c cfg, m map[string]string, s []string) int {
	return f(1, c.dir) + f(2, m["k"]) + f(3, "a"+c.dir) + f(len(s), string(rune(65)))
}
