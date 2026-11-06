package m

type Bed struct{ depth int }

func (b Bed) Deep() bool { return b.depth > 2 }
