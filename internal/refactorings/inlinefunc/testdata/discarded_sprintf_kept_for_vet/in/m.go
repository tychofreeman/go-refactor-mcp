package m

import "fmt"

func run(err error) {
	describe(err)
}

// describe's Sprintf calls err's Error method, which could do anything.
func describe(err error) string {
	fmt.Println("describing")
	return fmt.Sprintf("%v", err)
}
