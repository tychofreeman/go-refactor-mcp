package p

type T struct{ n int }

func (t T) Scale(by int) int { return t.n * by }
