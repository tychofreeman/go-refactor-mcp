package m

type Bed struct{ Heights map[string]int }

func find(b *Bed, name string) (h int, ok bool) {
	h, ok = b.Heights[name]
	if !ok {
		return
	}
	return h, true
}
