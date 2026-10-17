package m

import (
	"fmt"
)

func Shout(s string) {
	fmt.Println(loud(s))
}

func Quiet(s string) string { return s }
