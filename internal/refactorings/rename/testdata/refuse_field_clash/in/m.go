package m

type Pair struct {
	a, b int
}

func (p Pair) Sum() int { return p.a + p.b }
