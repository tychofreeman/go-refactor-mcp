package m

import "fmt"

func open(path string) ([]string, string) {
	return []string{path}, ""
}

func plan(path string) (out []string, refusal string) {
	fmt.Println("plan")
	l, why := open(path)
	if why != "" {
		return nil, why
	}
	fmt.Println(len(l))
	return l, ""
}
