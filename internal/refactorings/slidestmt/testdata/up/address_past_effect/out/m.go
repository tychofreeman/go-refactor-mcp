package m

import "log"

type W struct{ Charged []string }

func (w *W) Charge(to, ref string) {
	p := &w.Charged
	log.Printf("wallet charge to %s", to)
	*p = append(*p, to+": "+ref)
}
