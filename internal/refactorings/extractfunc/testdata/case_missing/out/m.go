package m

func renewable(kind string) bool {
	switch kind {
	case "book":
		return true
	case "atlas":
		return atlasRenewable()
	}
	return false
}

func atlasRenewable() bool {
	return false
}
