package m

func waterFern(bed string) string { return waterWith(bed, ": 1 L") } // the fern's

func waterWith(bed string, liters string) string { return bed + liters }

func waterOak(bed string) string { return waterWith(bed, ": 9 L") }

func use() {
	waterFern("a")
	waterOak("b")
}
