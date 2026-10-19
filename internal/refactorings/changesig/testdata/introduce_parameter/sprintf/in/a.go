package m

import "fmt"

func key(name string, n int) string { return "k:" + fmt.Sprintf("%s-%d", name, n) }

func Use() string { return key("a", 1) }
