package m

import "fmt"

func run(v any) {
	switch x := v.(type) {
	case int:
		step(x)
	}
}

func step(x int) {
	fmt.Println(x + 1)
}
