package m

func record(sent *[]string, s string) {
	*sent = append(*sent, s)
}

func other(sent []string, s string) []string {
	return append(sent, s+"!")
}
