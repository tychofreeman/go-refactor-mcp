package m

import "fmt"

type item struct {
	Size int
}

func Show(items []item) string { return fmt.Sprintf("%+v", items) }
