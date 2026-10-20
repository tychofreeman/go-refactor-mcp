package m

import (
	"fmt"
	"strings"
)

func f(xs []string) string { return fmt.Sprint(len(xs)) + strings.Join(xs, ",") }
