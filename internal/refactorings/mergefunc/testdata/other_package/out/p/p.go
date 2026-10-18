package p

import (
	"fmt"

	"example.com/m/textutil"
)

func Count(n int) string { return fmt.Sprintf("%d item%s", n, textutil.Plural(n)) }

func suffix() func(int) string { return textutil.Plural }
