package m

type Resetter interface {
	Reset()
	Name() string
}

type T struct{ n int }

func (t *T) Reset() { t.n = 0 }

type S struct {
	*T
}

func (S) Name() string { return "s" }

func Use(s S) { s.Reset() }
