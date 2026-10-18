package p

func Use(t *T) int {
	t.bump()
	return t.Get()
}
