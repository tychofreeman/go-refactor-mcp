package m

func Label(kind int, a, b string) string {
	switch kind {
	case 1:
		tag := "<" + a +
			b + ">"
		return tag
	}
	return ""
}
