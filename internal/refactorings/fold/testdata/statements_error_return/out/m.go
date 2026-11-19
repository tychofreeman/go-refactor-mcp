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
	h, err := find(b, name)
	if err != nil {
		return "", err
	}
	return fmt.Sprint(name, h), nil
}
