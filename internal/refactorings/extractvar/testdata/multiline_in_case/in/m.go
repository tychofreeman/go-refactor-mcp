package m

func Label(kind int, a, b string) string {
	switch kind {
	case 1:
		return "<" + a +
			b + ">"
	}
	return ""
}
