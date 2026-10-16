package m

func check(key any) bool {
	var s string
	var ok bool
	if s, ok = key.(string); !ok {
		return false
	}
	return s != ""
}
