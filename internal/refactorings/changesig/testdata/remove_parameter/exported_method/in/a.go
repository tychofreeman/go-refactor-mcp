package m

type T struct{}

func (T) Area(w, h, unused int) int { return w * h }

func Use() int { return T{}.Area(1, 2, 3) }
