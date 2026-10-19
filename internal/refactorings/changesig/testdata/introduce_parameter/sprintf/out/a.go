package m

import "fmt"

func key(name string, n int, key string) string { return "k:" + key }

func Use() string { return key("a", 1, fmt.Sprintf("%s-%d", "a", 1)) }
