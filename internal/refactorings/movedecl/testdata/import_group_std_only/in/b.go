package m

import (
	"fmt"
	"strconv"
)

func Base() int { return len(fmt.Sprint(strconv.Itoa(1))) }
