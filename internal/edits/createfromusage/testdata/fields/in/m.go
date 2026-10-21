package m

type W struct{ n int }

type Options struct {
	A bool
}

func f(w *W) bool {
	w.Name = "x"
	return w.Context == 3
}

func g() Options { return Options{A: true, Limit: 2.5} }
