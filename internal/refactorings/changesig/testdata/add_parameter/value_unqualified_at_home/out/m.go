package m

type Batch struct{ N int }

func Run(c int, b Batch) int { return c }

func local() int { return Run(1, Batch{N: 1}) }
