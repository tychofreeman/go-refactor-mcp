package m

func first(s string) byte {
	c := s[0]
	if s == "" {
		return 0
	}
	return c
}
