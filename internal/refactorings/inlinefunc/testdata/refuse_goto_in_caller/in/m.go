package m

import "fmt"

func run(n int) {
	if n > 0 {
		goto done
	}
	show()
done:
	fmt.Println("done")
}

func show() {
	m := 2
	fmt.Println(m)
}
