package m

import "fmt"

func keys[K comparable, V any, M ~map[K]V](m M) []K {
	var out []K
	out = collect[K, V, M](m, out)
	fmt.Println(len(out))
	return out
}

func collect[K comparable, V any, M ~map[K]V](m M, out []K) []K {
	for k := range m {
		out = append(out, k)
	}
	return out
}
