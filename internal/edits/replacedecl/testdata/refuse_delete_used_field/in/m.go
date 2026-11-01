package m

type Bed struct {
	Name string
	Rows int
}

func Label(b Bed) string { return b.Name }
