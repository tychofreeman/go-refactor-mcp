package m

type Plan struct{ items []int }

func (pl *Plan) Add(n int) { pl.items = append(pl.items, n) }
