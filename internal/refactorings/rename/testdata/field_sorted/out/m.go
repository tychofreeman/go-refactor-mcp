package m

import "sort"

type rec struct {
	Title string
}

func Sorted(rs []rec) bool {
	sort.Slice(rs, func(i, j int) bool { return rs[i].Title < rs[j].Title })
	return sort.SliceIsSorted(rs, func(i, j int) bool { return rs[i].Title < rs[j].Title })
}
