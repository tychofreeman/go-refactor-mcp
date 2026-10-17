package m

import "fmt"

func run() {
	type pair struct{ a, b int }
	fmt.Println(pair{1, 2})
}
