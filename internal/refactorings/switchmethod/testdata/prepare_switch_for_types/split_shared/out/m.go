package m

type Item struct {
	Title string
	Kind  any
}

type Kind interface {
	Renewable(it Item) bool
}

func Renewable(it Item) bool {
	if k, ok := it.Kind.(Kind); ok {
		return k.Renewable(it)
	}
	switch it.Kind {
	case "book":
		if it.Title == "" {
			return false
		}
		return true
	case "dvd":
		if it.Title == "" {
			return false
		}
		return true
	case "atlas":
		return false
	case "map":
		return false
	}
	return false
}
