package m

type Pt struct{ X, Y int }

func AtOrigin(p Pt) bool {
	if p == (Pt{0, 0}) {
		return true
	}
	return false
}
