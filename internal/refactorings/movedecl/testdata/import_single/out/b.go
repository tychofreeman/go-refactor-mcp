package m

import (
	"fmt"
	"strings"

	"example.com/m/u"
)

func Base() int { return len(fmt.Sprint(1)) }

func Loud() string { return strings.ToUpper(u.Name()) }
