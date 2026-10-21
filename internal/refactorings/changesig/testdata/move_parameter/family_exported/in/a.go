package m

type Labeler interface{ Label(n int, s string) string }

type T struct{}

func (T) Label(n int, s string) string { return s }

var _ Labeler = T{}
