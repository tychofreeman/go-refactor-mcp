package m

import (
	"fmt"
	"strings"
)

func shout(s string) {
	fmt.Println(s + "!")
	_ = strings.ToUpper
}

func whisper(s string) string { return s }
