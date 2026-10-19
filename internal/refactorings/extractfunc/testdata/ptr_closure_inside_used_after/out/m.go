package m

import "fmt"

type checker struct{ report func(string) }

func (c checker) run(s string) { c.report(s) }

func check(items []string) {
	var errs []string
	checkAll(&errs, items)

	fmt.Println(errs)
}

func checkAll(errs *[]string, items []string) {
	c := checker{report: func(msg string) { *errs = append(*errs, msg) }}
	for _, it := range items {
		c.run(it)
	}
}
