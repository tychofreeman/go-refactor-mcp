package m

type bed struct{ n int }

func depth(b *bed) int {
	println("watering")
	q := &b.n
	return *q
}
