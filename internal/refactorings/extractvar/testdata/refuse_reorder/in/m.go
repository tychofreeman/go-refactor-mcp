package m

func read() int {
	println("read")
	return 1
}
func read2() int {
	println("read2")
	return 2
}

func Both() int {
	return read() - read2()
}
