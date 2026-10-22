package m

type W struct{ Charged []string }

func wait(ready chan bool) { <-ready }

func (w *W) Charge(ready chan bool, ref string) {
	wait(ready)
	record(&w.Charged, ref)
}

func record(charged *[]string, s string) { *charged = append(*charged, s) }

func Use(w *W) { w.Charge(nil, "b") }
