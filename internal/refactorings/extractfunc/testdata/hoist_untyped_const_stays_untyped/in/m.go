package m

import "fmt"

func run() {
	const big = 1 << 40
	f := big / 3.0
	fmt.Println(f)
}
