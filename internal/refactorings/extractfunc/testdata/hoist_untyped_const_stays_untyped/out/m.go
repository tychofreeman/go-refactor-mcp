package m

import "fmt"

const big = 1 << 40

func run() {
	show()
}

func show() {
	f := big / 3.0
	fmt.Println(f)
}
