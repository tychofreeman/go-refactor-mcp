package m

func once(f func()) { f() }

func Feed(n int) int {
	once(func() {
		defer println("fed")
		n *= 2
	})
	return n
}
