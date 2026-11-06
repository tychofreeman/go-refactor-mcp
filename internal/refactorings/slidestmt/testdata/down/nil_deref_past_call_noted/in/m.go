package m

type bed struct{ n int }

func depth(b *bed) int {
	q := &b.n
	println("watering")
	return *q
}
