package m

import "fmt"

func run(err error) {
	fmt.Println("describing")
	_ = fmt.Sprintf("%v", err)
}
