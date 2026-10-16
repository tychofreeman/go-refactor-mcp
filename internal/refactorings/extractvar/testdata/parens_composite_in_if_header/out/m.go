package m

type Pt struct{ X, Y int }

func AtOrigin(p Pt) bool {
	origin := Pt{0, 0}
	if p == origin {
		return true
	}
	return false
}
