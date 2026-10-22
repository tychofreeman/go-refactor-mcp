package m

import "fmt"

func greet(who, name string) string { return fmt.Sprintf("%s: hello %s", who, name) }

func Bot(n string) string { return fmt.Sprintf("bot: hello %s", n) }

func Both() string { return fmt.Sprintf("me: hello you") }

func Percent(n string) string { return fmt.Sprintf("100%%: hello %s", n) }

func Count(n string) string { return fmt.Sprintf("%d: hello %s", 3, n) }
