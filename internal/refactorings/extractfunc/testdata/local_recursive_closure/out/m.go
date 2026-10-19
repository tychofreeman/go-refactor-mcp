package m

import "sort"

func order(deps map[string][]string) []string {
	out := visitAll(deps)

	return append(out, "end")
}

func visitAll(deps map[string][]string) []string {
	var out []string
	seen := map[string]bool{}
	var visit func(n string)
	visit = func(n string) {
		if seen[n] {
			return
		}
		seen[n] = true
		for _, d := range deps[n] {
			visit(d)
		}
		out = append(out, n)
	}
	keys := make([]string, 0, len(deps))
	for k := range deps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		visit(k)
	}
	return out
}
