package m

import (
	"fmt"
	"log"
)

func last(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func note(name string) {
	log.Print("start")
	fmt.Println(name)
}

func Use(s string) {
	log.Print("start")
	fmt.Println(last(s, 4))
}
