package m

import "fmt"

type renderArg = string

func render(layout ...renderArg) string { return fmt.Sprint(len(layout)) }

func use(names []string) string { return render("a", "b") + render() }
