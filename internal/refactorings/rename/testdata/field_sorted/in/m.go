package m

import "sort"

type rec struct {
	Name string
}

func Sorted(rs []rec) bool {
	sort.Slice(rs, func(i, j int) bool { return rs[i].Name < rs[j].Name })
	return sort.SliceIsSorted(rs, func(i, j int) bool { return rs[i].Name < rs[j].Name })
}
