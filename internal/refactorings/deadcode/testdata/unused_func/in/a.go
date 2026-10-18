package m

import (
	"fmt"
	"strings"
)

func Keep() string { return strings.ToUpper("a") }

// old printed things.
func old(n int) {
	fmt.Println(n)
	old(n - 1)
}

func Other() int { return 1 }
