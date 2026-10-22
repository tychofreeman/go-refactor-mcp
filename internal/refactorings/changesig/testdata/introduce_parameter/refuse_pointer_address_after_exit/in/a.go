package m

import "log"

type W struct{ Charged []string }

func (w *W) Charge(to, ref string) {
	if to == "" {
		log.Fatalf("no one to charge for %s", ref)
	}
	record(&w.Charged, to+": "+ref)
}

func record(charged *[]string, s string) { *charged = append(*charged, s) }

func Use(w *W) { w.Charge("a", "b") }
