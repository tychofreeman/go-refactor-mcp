package m

import "fmt"

func run(n int) {
	if n > 0 {
		goto done
	}
	fmt.Println("positive")
done:
	fmt.Println("done")
}
