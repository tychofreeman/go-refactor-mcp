package m

type Soil struct{ depth int }

type Bed struct{ Soil }

func (b Bed) Deep() bool { return b.depth > 2 }
