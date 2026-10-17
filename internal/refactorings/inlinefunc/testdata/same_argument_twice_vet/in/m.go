package m

import "fmt"

func run(line string) {
	p := common(line, line)
	fmt.Println(p)
}

func common(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	return a[:i]
}
