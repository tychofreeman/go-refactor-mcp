package m

var log []string

func k() int {
	log = append(log, "k")
	return 0
}

func f(n int) int { return n + 1 }

func run(m map[int]int, n int) { m[k()] = f(n) }
