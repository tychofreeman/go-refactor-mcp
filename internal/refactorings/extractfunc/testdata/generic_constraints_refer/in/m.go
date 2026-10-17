package m

import "fmt"

func keys[K comparable, V any, M ~map[K]V](m M) []K {
	var out []K
	for k := range m {
		out = append(out, k)
	}
	fmt.Println(len(out))
	return out
}
