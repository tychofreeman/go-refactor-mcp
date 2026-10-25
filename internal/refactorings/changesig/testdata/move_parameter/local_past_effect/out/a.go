package m

type list struct{ words []string }

func (l *list) next() string {
	w := l.words[0]
	l.words = l.words[1:]
	return w
}

func join(b, a string) string { return a + ">" + b }

func Use(l *list) string {
	first := l.next()
	return join(l.next(), first)
}
