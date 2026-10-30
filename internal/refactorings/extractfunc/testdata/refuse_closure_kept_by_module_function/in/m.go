package m

var later []func()

// each calls f now and keeps it to call again later.
func each(f func()) {
	f()
	later = append(later, f)
}

func count() int {
	n := 0
	each(func() { n++ })
	return n
}
