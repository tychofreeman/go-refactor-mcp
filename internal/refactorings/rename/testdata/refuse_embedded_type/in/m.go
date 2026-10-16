package m

type Base struct{ ID int }

type Derived struct {
	Base
	Name string
}

func ID(d Derived) int { return d.Base.ID }
