package m

import "fmt"

type checker struct{ report func(string) }

func (c checker) run(s string) { c.report(s) }

func check(items []string) {
	var errs []string
	c := checker{report: func(msg string) { errs = append(errs, msg) }}
	for _, it := range items {
		c.run(it)
	}

	fmt.Println(errs)
}
