package m

import "log"

type W struct{ Charged []string }

func (w *W) Charge(to, ref string) {
	log.Printf("wallet charge to %s", to)
	record(&w.Charged, to+": "+ref)
}

func record(charged *[]string, s string) { *charged = append(*charged, s) }

func Use(w *W) { w.Charge("a", "b") }
