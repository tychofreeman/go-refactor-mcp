package m

import "strconv"

type Bed struct {
	Width, Depth int
	Edged        bool
}

func area(b *Bed) int {
	a := b.Width * b.Depth
	if b.Edged {
		a -= 2 * (b.Width + b.Depth)
	}
	return a
}

func (b *Bed) Label() string {
	size := b.Width * b.Depth
	if b.Edged {
		size -= 2 * (b.Width + b.Depth)
	}
	return strconv.Itoa(size) + " square feet"
}
