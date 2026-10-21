package m

type SMS struct{}

func cut(t string, n int) string {
	if len(t) <= n {
		return t
	}
	return t[:n]
}

func (s *SMS) short(t string) string {
	return cut(t, 20)
}

func (s *SMS) tiny(t string) string {
	return cut(t, 5)
}
