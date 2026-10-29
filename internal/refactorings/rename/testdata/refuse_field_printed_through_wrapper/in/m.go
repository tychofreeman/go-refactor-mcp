package m

import "fmt"

type rec struct {
	Name string
}

func logf(format string, args ...any) { fmt.Printf(format, args...) }

func Show(r rec) { logf("%+v", r) }
