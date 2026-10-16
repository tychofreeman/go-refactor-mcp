package m

type Pt struct{ X, Y int }

func X(p *Pt) int {
	return (*p).X
}
