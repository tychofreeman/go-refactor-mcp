package m

func f(a, b int) int { return a - b }

func bump(p *int) int { *p++; return *p }

func Use() int {
	x := 1
	p := &x
	return f(x, bump(p))
}
