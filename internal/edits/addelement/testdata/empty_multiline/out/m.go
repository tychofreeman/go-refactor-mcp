package m

type Plan struct{ Done bool }

func New() *Plan {
	return &Plan{
		Done: true,
	}
}
