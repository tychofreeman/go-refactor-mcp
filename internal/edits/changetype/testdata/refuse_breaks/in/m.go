package m

type renderArg = string

func first(layout ...renderArg) string { return layout[0] }

func use(names []string) string { return first(names...) }
