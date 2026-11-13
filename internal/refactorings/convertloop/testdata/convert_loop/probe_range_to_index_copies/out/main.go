package main

import "fmt"

type item struct{ n int }

func main() {
	items := []item{{1}, {2}, {3}}
	total := 0
	for i := 0; i < len(items); i++ {
		it := items[i]
		it.n *= 10
		total += it.n + items[i].n
		if i == 1 {
			break
		}
	}
	fmt.Println(total, items)
}
