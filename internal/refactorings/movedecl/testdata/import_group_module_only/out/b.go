package m

import (
	"strings"

	"example.com/m/u"
)

func Base() int { return len(u.Name()) }

func Loud() string { return strings.ToUpper(u.Name()) }
