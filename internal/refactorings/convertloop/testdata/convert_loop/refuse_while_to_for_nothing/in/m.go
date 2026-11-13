package m

func drain(ch chan int) {
	for len(ch) > 0 {
		<-ch
	}
}
