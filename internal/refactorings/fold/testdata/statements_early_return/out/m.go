package m

import "errors"

type Bed struct{ Heights map[string]int }

func find(b *Bed, name string) (int, error) {
	h, ok := b.Heights[name]
	if !ok {
		return 0, errors.New("no " + name)
	}
	return h, nil
}

func (b *Bed) Height(name string) (int, error) {
	return find(b, name)
}
