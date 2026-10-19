package m

import "fmt"

func count(keys []string) int {
	n := 0
	var report func()
	report = func() { fmt.Println(n) }
	go report()
	for range keys {
		n++
	}

	return n
}
