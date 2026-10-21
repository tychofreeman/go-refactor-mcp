package m

type Batch struct{ N int }

func Run(n int, bt Batch) int {
	b := bt
	return b.N
}

func local() int { return Run(1, Batch{N: 1}) }
