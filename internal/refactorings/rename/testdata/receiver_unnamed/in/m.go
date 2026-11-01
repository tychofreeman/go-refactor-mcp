package m

type Bed struct{ Rows int }

func (*Bed) Size() int { return 1 }
