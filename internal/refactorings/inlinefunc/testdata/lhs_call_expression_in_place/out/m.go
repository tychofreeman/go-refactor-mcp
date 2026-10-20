package m

var log []string

func k() int {
	log = append(log, "k")
	return 0
}

func run(m map[int]int, n int) { m[k()] = n + 1 }
