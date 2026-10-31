package m

func waterFern(bed string) string { return bed + ": 1 L" } // the fern's

func waterOak(bed string) string { return bed + ": 9 L" }

func use() {
	waterFern("a")
	waterOak("b")
}
