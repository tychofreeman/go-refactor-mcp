package m

import "fmt"

func run(v any) {
	switch x := v.(type) {
	case int:
		fmt.Println(x + 1)
	}
}
