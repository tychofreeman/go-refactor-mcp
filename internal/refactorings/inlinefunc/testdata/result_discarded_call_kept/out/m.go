package m

import "fmt"

func run() {
	fmt.Println("logging")
	_ = fmt.Sprint("hi") == "" && len("hi") > 0 || count("hi") > 0
}

func count(s string) int { return len(s) }
