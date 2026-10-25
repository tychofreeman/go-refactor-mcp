package m

type Kind interface {
	shelve(into map[string]int)
}

func Shelve(k any, shelves map[string]int) {
	if k2, ok := k.(Kind); ok {
		k2.shelve(shelves)
	} else {
		switch k {
		case "book":
			shelves["stacks"]++
		case "dvd":
			shelves["media"]++
		default:
			shelves["returns"]++
		}
	}
}
