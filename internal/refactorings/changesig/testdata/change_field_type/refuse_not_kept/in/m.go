package m

type Box struct{ v int }

func g(b *Box) int { return b.v + 1 }
