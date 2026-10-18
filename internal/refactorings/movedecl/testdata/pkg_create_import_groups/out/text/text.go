package text

import (
	"strconv"
	"strings"

	"example.com/m/util"
)

func Show(n int) string { return strings.Repeat(strconv.Itoa(util.Twice(n)), 2) }
