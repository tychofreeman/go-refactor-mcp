package m

func record(log *[]string, n *int, s string) {
	*log = append(*log, s)
	*n++
}

func other(log *[]string, n *int, s string) {
	*log = append(*log, s)
	*n--
}
