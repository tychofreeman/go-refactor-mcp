package m

func once(f func()) { f() }

func Feed(n int) int {
	defer println("fed")
	n *= 2
	return n
}
