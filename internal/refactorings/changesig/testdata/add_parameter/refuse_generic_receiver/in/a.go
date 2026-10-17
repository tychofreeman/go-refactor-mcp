package m

type Getter interface{ Get() int }

type Box[V any] struct{ v V }

func (b Box[V]) Get() V { return b.v }
