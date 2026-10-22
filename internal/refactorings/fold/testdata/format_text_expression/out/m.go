package m

import "fmt"

func greet(who, name string) string { return fmt.Sprintf("%s: hello %s", who, name) }

func Bot(n string) string { return greet("bot", n) }

func Both() string { return greet("me", "you") }

func Percent(n string) string { return fmt.Sprintf("100%%: hello %s", n) }

func Count(n string) string { return fmt.Sprintf("%d: hello %s", 3, n) }
