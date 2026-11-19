package m

type Bed struct {
	Heights map[string]int
	Err     error
}

func find(b *Bed, name string) (int, error) {
	h, ok := b.Heights[name]
	if !ok {
		return 0, b.Err
	}
	return h, nil
}

func (b *Bed) Label(name string) (int, error) {
	h, ok := b.Heights[name]
	if !ok {
		return 0, b.Err
	}
	return h * 2, nil
}
