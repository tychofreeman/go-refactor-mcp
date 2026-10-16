package m

import "strings"

type W struct{ b strings.Builder }

func (w *W) Add(s string) {
	w.b.WriteString(s)
}
