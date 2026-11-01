package m

var b = 2

type Bed struct{ Rows int }

func (*Bed) Size() int { return b }
