package m

import "fmt"

func show(v int) string { return fmt.Sprint(v) }

func Show() string { return show(1) }
