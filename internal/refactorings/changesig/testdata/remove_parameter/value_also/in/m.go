package m

// Matcher matches.
type Matcher struct{ depth int }

func (m *Matcher) walk(n, depth int) int {
	d := depth
	if n == 0 {
		return d
	}
	return d + m.step(n-1, depth)
}

func (m *Matcher) step(n, depth int) int { return depth + m.walk(n, depth) }

// Run runs.
func (m *Matcher) Run() int { return m.walk(3, m.depth) }
