package m

type mover struct{ n int }

type inner struct{}

func (inner) size() int { return 1 }

type outer struct {
	mover
	deep
}

type deep struct{ inner }

func size(m *mover) int { return m.n }

func Use(o outer) int { return o.size() }
