package main

import "fmt"

var log []string

func note(kind string, n int) {
	log = append(log, fmt.Sprintf("%s:%d", kind, n))
}

func water(n int) {
	log = append(log, fmt.Sprintf("%s:%d", "water", n))
}

func feed(n int) {
	log = append(log, fmt.Sprintf("%s:%d", "feed", n+1))
}

func main() {
	water(2)
	feed(3)
	note("prune", 1)
	fmt.Println(log)
}
