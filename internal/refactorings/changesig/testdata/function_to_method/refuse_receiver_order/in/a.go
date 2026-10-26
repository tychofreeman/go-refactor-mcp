package m

type mover struct{ n int }

var count int

func size(k int, m *mover) int { return m.n + k }

func next() int { count++; return count }

func pick() *mover { count *= 2; return &mover{n: count} }

func use() int { return size(next(), pick()) }
