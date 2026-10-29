package m

type store struct {
	names []string
	index map[string]int
}

// byName is the index of names, made on first use.
func (s *store) byName() map[string]int {
	if s.index == nil {
		s.index = map[string]int{}
		for i, n := range s.names {
			s.index[n] = i
		}
	}
	return s.index
}

type finder struct{ s *store }

func (f *finder) find(name string, index map[string]int) int { return index[name] }

func (f *finder) Both(a, b string) int { return f.find(a, f.s.byName()) + f.find(b, f.s.byName()) }
