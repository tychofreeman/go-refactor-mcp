package m

import "fmt"

func run(n int) {
	describe(n)
}

func describe(n int) string {
	fmt.Println("describing")
	return fmt.Sprintf("%d", n)
}
