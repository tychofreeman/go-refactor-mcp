package m

import "fmt"

func check() error { return nil }

func run() {
	if err := check(); err == nil {
		fmt.Println("ok")
	}
}
