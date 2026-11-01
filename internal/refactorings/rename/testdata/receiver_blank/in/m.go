package m

type Bed struct{ Rows int }

func (_ Bed) Size() int { return 1 }
