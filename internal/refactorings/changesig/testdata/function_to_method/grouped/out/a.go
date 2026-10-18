package m

type mover struct{ n int }

func (a *mover) both(b *mover) int { return a.n + b.n }

func Use(x, y *mover) int { return x.both(y) }
