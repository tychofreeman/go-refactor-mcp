package m

var total int

func save(x int) {
	if x < 0 {
		return
	}
	total += x
}

func use() {
	save(1)
}
