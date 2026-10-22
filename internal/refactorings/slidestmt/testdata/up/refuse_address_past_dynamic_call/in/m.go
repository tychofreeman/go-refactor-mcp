package m

type W struct{ Charged []string }

func (w *W) Charge(to, ref string, notify func(string)) {
	notify(to)
	p := &w.Charged
	*p = append(*p, to+": "+ref)
}
