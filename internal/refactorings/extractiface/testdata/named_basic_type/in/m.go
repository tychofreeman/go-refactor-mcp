package m

// Litres is an amount of water.
type Litres int

// Half is half of l.
func (l Litres) Half() Litres { return l / 2 }
