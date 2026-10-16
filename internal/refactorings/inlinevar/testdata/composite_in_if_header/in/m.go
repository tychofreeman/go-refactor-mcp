package m

type Pt struct{ X, Y int }

func AtOrigin(p Pt) bool {
	o := Pt{0, 0}
	if p == o {
		return true
	}
	return false
}
