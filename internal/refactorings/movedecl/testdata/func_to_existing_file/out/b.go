package m

import (
	"fmt"
	"strings"
)

func Say(s string) { fmt.Println(s) }

// loud upper-cases s.
func loud(s string) string {
	return strings.ToUpper(s) + "!"
}
