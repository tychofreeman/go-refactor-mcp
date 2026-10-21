package m

import "fmt"

type renderArg = any

func render(layout ...renderArg) string { return fmt.Sprint(len(layout)) }

func use(names []string) string { return render("a", "b") + render() }
