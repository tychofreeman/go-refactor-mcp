package m

import "fmt"

func run(line string) {
	b := line
	i := 0
	for i < len(line) && i < len(b) && line[i] == b[i] {
		i++
	}
	p := line[:i]
	fmt.Println(p)
}
