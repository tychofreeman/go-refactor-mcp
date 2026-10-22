package m

import "log"

type W struct{ Charged []string }

func (w *W) Charge(to, ref string) {
	log.Fatalf("wallet charge to %s", to)
	p := &w.Charged
	*p = append(*p, to+": "+ref)
}
