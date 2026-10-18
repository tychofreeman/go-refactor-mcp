package m

import (
	"strings"

	"example.com/m/u"
)

func Base() int { return 1 }

func Loud() string { return strings.ToUpper(u.Name()) }
