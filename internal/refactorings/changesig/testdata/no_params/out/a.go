package m

var count int

func tick(step int) { count++ }

func Run() { tick(1); tick(1) }
