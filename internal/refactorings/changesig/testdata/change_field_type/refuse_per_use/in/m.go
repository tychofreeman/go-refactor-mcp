package m

type Box struct{ v int }

func f(b *Box) *int { return &b.v }

func g(b *Box) int { return b.v + 1 }
