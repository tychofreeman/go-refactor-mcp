package m

type Message struct{}

type Batch struct {
	N       int
	Message Message
}

func Run(c int) int { return c }

func local() int { return Run(1) }
