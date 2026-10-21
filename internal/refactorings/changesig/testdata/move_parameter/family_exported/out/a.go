package m

type Labeler interface{ Label(s string, n int) string }

type T struct{}

func (T) Label(s string, n int) string { return s }

var _ Labeler = T{}
