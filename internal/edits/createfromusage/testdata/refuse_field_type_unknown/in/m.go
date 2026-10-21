package m

type W struct{ n int }

func f(w *W) {
	_ = w.Thing
}
