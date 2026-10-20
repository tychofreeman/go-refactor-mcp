package m

func both(x bool, b int) bool { return x && b > 0 }

func caller(flag bool, i int) bool {
	var arr [3]int
	r := both(flag, arr[i])
	return r
}
