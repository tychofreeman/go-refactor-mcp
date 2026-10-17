package m

import (
	"fmt"
	"strings"
)

func Shout(s string) {
	fmt.Println(loud(s))
}

// loud upper-cases s.
func loud(s string) string {
	return strings.ToUpper(s) + "!"
}

func Quiet(s string) string { return s }
