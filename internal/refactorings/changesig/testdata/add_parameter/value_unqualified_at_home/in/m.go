package m

type Batch struct{ N int }

func Run(c int) int { return c }

func local() int { return Run(1) }
