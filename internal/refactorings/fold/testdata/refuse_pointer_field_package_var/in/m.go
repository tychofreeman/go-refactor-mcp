package m

type R struct{ Errs []string }

var cur = &R{}

func reset() { cur = &R{} }

func add(errs *[]string, msg string) {
	reset()
	*errs = append(*errs, msg)
}

func Add(msg string) {
	reset()
	cur.Errs = append(cur.Errs, msg)
}
