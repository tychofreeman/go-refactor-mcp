package q

type box struct{ n int }

func (b box) get() int { return b.n + offset }

const offset = 1

// Two is two.
func Two() int { return box{1}.get() }
