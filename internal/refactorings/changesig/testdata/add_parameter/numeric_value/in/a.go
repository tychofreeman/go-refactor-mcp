package m

var count int

func tick() { count++ }

func Run() { tick(); tick() }
