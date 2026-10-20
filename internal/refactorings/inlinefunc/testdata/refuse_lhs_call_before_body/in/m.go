package m

var log []string

func k() int {
	log = append(log, "k")
	return 0
}

func f() int {
	log = append(log, "f")
	return 1
}

func run() {
	m := map[int]int{}
	m[k()] = f()
	_ = m
}
