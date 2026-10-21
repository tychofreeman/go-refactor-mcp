package m

type Batch struct{ N int }

func Run(n int) int {
	b := Batch{N: n}
	return b.N
}

func local() int { return Run(1) }
