package m

func Set(m map[string]int, key string) {
	k := key + "!"
	m[k] = 1
}
