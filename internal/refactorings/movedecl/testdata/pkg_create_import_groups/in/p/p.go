package p

import (
	"strconv"
	"strings"

	"example.com/m/util"
)

func Show(n int) string { return strings.Repeat(strconv.Itoa(util.Twice(n)), 2) }

func Keep() int { return 1 }
