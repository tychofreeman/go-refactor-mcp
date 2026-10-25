package m

type Kind interface {
	shelve() int
}

func Shelve(k any, shelves map[string]int) {
	switch k {
	case "book":
		shelves["stacks"]++
	case "dvd":
		shelves["media"]++
	default:
		shelves["returns"]++
	}
}
