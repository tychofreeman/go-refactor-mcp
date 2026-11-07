package m

// Halver has the method Half of Litres.
type Halver interface {
	Half() Litres
}

var _ Halver = (*Litres)(nil)

// Litres is an amount of water.
type Litres int

// Half is half of l.
func (l Litres) Half() Litres { return l / 2 }
