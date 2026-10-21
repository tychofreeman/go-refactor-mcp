package m

var total int

func save(x int) error {
	if x < 0 {
		return nil
	}
	total += x
	return nil
}

func use() {
	save(1)
}
