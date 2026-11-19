package m

type Plant struct{ Height int }

type Bed struct{ Rows int }

// Size reads a clause's own v: each clause of the type switch declares
// one, of that clause's type.
func Size(x any) int {
	switch v := x.(type) {
	case *Plant:
		return v.Height
	case *Bed:
		return v.Rows
	}
	return 0
}
