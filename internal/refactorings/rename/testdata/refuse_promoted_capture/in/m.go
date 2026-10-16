package m

type Inner struct{ Y int }

type Outer struct {
	Inner
	X int
}

func Get(o Outer) int { return o.X + o.Y }
