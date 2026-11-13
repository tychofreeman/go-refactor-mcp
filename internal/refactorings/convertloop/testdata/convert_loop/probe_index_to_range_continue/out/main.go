package main

import "fmt"

func main() {
	words := []string{"a", "", "b", "stop", "c"}
	out := ""
outer:
	for i, word := range words {
		for j := 0; j < 2; j++ {
			if word == "" {
				continue outer
			}
			if word == "stop" {
				break outer
			}
			out += fmt.Sprint(i, j, word)
		}
	}
	fmt.Println(out)
}
