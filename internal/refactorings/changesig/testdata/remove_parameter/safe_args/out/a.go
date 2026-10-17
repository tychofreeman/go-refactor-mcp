package m

type cfg struct{ dir string }

func f(n int) int { return n }

func Use(c cfg, m map[string]string, s []string) int {
	return f(1) + f(2) + f(3) + f(len(s))
}
