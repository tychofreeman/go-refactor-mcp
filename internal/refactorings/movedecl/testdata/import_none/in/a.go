package m

import (
	"strings"

	"example.com/m/u"
)

func Loud() string { return strings.ToUpper(u.Name()) }

func Use() string { return Loud() }
