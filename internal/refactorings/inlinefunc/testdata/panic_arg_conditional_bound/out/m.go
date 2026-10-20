package m

func caller(flag bool, i int) bool {
	var arr [3]int
	b := arr[i]
	r := flag && b > 0
	return r
}
