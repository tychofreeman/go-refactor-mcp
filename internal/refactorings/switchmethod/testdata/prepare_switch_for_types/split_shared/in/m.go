package m

type Item struct {
	Title string
	Kind  any
}

func Renewable(it Item) bool {
	switch it.Kind {
	case "book", "dvd":
		if it.Title == "" {
			return false
		}
		return true
	case "atlas", "map":
		return false
	}
	return false
}
