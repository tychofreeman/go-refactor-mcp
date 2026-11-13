package main

import "fmt"

func main() {
	words := []string{"a", "", "b", "stop", "c"}
	out := ""
outer:
	for i := 0; i < len(words); i++ {
		for j := 0; j < 2; j++ {
			if words[i] == "" {
				continue outer
			}
			if words[i] == "stop" {
				break outer
			}
			out += fmt.Sprint(i, j, words[i])
		}
	}
	fmt.Println(out)
}
