package m

type Shape interface{ Area() float64 }

type Square struct{ s float64 }

func (q Square) Area() float64 { return q.s * q.s }
