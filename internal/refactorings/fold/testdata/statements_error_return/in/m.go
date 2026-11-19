package m

import (
	"errors"
	"fmt"
)

type Bed struct{ Heights map[string]int }

func find(b *Bed, name string) (int, error) {
	h, ok := b.Heights[name]
	if !ok {
		return 0, errors.New("no " + name)
	}
	return h, nil
}

func (b *Bed) Label(name string) (string, error) {
	h, ok := b.Heights[name]
	if !ok {
		return "", errors.New("no " + name)
	}
	return fmt.Sprint(name, h), nil
}
