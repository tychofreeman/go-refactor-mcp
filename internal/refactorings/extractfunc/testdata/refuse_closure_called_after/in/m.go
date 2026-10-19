package m

func order(deps map[string][]string, keys []string) []string {
	var out []string
	var visit func(n string)
	visit = func(n string) {
		for _, d := range deps[n] {
			visit(d)
		}
		out = append(out, n)
	}
	for _, k := range keys {
		visit(k)
	}

	visit("last")
	return out
}
