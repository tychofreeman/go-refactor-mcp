package m

import "fmt"

func open(path string) ([]string, string) {
	return []string{path}, ""
}

func plan(path string) (out []string, refusal string) {
	fmt.Println("plan")
	l, refusal2 := lines(path)
	if refusal2 != "" {
		return nil, refusal2
	}
	fmt.Println(len(l))
	return l, ""
}

func lines(path string) ([]string, string) {
	l, why := open(path)
	if why != "" {
		return nil, why
	}
	return l, ""
}
