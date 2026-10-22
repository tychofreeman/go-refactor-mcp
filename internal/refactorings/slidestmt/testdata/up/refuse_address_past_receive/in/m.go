package m

type W struct{ Charged []string }

func (w *W) Charge(ready chan bool, ref string) {
	<-ready
	p := &w.Charged
	*p = append(*p, ref)
}
