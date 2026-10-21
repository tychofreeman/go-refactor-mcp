package m

type Resetter interface {
	Reset(to int)
	Name() string
}

type T struct{ n int }

func (t *T) Reset(to int) { t.n = 0 }

type S struct {
	*T
}

func (S) Name() string { return "s" }

func Use(s S) { s.Reset(0) }
