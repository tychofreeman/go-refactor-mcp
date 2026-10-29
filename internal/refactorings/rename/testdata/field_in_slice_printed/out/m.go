package m

import "fmt"

type item struct {
	Length int
}

func Show(items []item) string { return fmt.Sprintf("%+v", items) }
