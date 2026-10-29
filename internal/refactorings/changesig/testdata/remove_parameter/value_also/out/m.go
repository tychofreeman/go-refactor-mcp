package m

// Matcher matches.
type Matcher struct{ depth int }

func (m *Matcher) walk(n int) int {
	d := m.depth
	if n == 0 {
		return d
	}
	return d + m.step(n-1)
}

func (m *Matcher) step(n int) int { return m.depth + m.walk(n) }

// Run runs.
func (m *Matcher) Run() int { return m.walk(3) }
