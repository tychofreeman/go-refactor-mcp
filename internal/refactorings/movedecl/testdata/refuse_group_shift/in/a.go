package m

const (
	first  = "a"
	second = iota
)

func Use() (string, int) { return first, second }
