package m

type mover struct{ n int }

func both(a, b *mover) int { return a.n + b.n }

func Use(x, y *mover) int { return both(x, y) }
