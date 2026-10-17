package m

type T struct{ s string }

func (t T) Len() int { return len(t.s) }
