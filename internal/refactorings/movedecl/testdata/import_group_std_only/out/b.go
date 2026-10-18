package m

import (
	"fmt"
	"strconv"
	"strings"

	"example.com/m/u"
)

func Base() int { return len(fmt.Sprint(strconv.Itoa(1))) }

func Loud() string { return strings.ToUpper(u.Name()) }
