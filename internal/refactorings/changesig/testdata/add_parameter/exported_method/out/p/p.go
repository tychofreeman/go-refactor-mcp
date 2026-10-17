package p

type T struct{ n int }

func (t T) Scale(by int, unit string) int { return t.n * by }
