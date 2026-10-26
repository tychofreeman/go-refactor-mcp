package m

type item struct{ title string }

func (it item) label() string { return it.title }

type book struct{}

func label(it item, b book) string { return "[" + it.title + "]" }
