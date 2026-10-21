package m

type U struct{ X, Y string }

func (u U) a() string { return u.X }

func (u U) b() string { return u.Y }
