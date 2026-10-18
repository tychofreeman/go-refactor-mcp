package m

import (
	"fmt"
	"strings"
)

func first() string { return strings.ToUpper(second()) }

func second() string { return fmt.Sprint(2) }
