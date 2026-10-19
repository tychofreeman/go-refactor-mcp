package m

type Plan struct {
	Summary string
	Notes   []string
	Done    bool
}

type pair struct{ a, b int }

func New() *Plan {
	return &Plan{
		Done:    true,
		Summary: "s",
		Notes:   nil,
	}
}

func Short() Plan { return Plan{Summary: "s"} }

func Pairs() []pair { return []pair{{1, 2}} }
