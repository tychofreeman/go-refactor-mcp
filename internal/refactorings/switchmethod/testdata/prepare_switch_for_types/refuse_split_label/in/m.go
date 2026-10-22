package m

func Shelve(k any, shelves map[string]int) {
	switch k {
	case "book", "dvd":
	retry:
		shelves["stacks"]++
		if shelves["stacks"] < 2 {
			goto retry
		}
	}
}
