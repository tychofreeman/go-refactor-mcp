package m

type W struct{ Charged []string }

var notify = func(string) {}

func (w *W) Charge(to, ref string) {
	notify(to)
	record(&w.Charged, to+": "+ref)
}

func record(charged *[]string, s string) { *charged = append(*charged, s) }

func Use(w *W) { w.Charge("a", "b") }
